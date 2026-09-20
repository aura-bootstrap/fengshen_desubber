#!/usr/bin/env python3
"""DiffuEraser sidecar for desub: diffusion-tier inpainting of one chunk of
the band strip, same PNG-directory contract as propainter_infer.py, so the Go
caller selects it with `--propainter-script scripts/diffueraser_infer.py`.

  --strip DIR   RGB band frames, %05d.png numbered from 0 over the whole job
  --masks DIR   gray masks (255 = inpaint), same numbering
  --out DIR     repaired frames for the requested range, same numbering
  --start N     first frame index (relative to the job, 0-based)
  --count M     number of frames to process
  --fps F       frame rate (chunk and mask videos must share it exactly)

DiffuEraser itself is NOT bundled: set DIFFUERASER_HOME to a checkout of
https://github.com/lixiaowen-xw/DiffuEraser with weights under weights/
(HF lixiaowen/diffuEraser + sd-1.5 / PCM_Weights / propainter / sd-vae-ft-mse).

Env knobs: DIFFUERASER_HOME (required), DIFFUERASER_ENTRY (default
run_diffueraser.py), DIFFUERASER_EXTRA_ARGS (appended verbatim).

STATUS: written against the upstream README but NOT integration-tested (needs
GPU: 12G@640x360 … 33G@1280x720). Bring-up notes:
  - upstream run_diffueraser.py gained argparse (--input_video/--input_mask/
    --save_path/...) in our checkout; if yours lacks it, set DIFFUERASER_ENTRY
    to your own wrapper.
  - mask video polarity: white = region to erase (same as ProPainter);
    verify on the first run.
  - video and mask must be mp4 with identical fps (upstream misalignment
    guard); we encode both from the same frame count with -r == --fps.
"""
import argparse
import os
import shutil
import subprocess
import sys


def fail(msg):
    print(f"diffueraser: {msg}", file=sys.stderr)
    sys.exit(3)


def _stripe_shift(base, mk):
    """Estimate a phase-aligned translation for periodic texture crossing the
    mask edge. Returns (dx, dy) of an integer number of stripe periods toward
    the side with the strongest texture, or (0, 0) when the neighbourhood is
    not periodic (plain cloth, skin, background)."""
    import cv2
    import numpy as np
    if not mk.any():
        return 0, 0
    g = cv2.cvtColor(base, cv2.COLOR_BGR2GRAY)
    e = cv2.GaussianBlur(
        np.abs(cv2.Laplacian(g, cv2.CV_64F)).astype(np.float32), (0, 0), 2.0)
    ring = (cv2.dilate(mk, np.ones((21, 21), np.uint8)) > 0) & (mk == 0)
    if not ring.any():
        return 0, 0
    ys, xs = np.nonzero(mk)
    cx, cy = (xs.min() + xs.max()) / 2, (ys.min() + ys.max()) / 2
    yy, xx = np.nonzero(ring)
    best_side, best_e = None, 0.0
    for name, sel in (("right", xx > cx), ("left", xx <= cx),
                      ("bottom", yy > cy), ("top", yy <= cy)):
        if not sel.any():
            continue
        v = float(e[yy[sel], xx[sel]].mean())
        if v > best_e:
            best_side, best_e = name, v
    h, w = g.shape
    if best_side == "right":
        band = g[:, xs.max() + 1:min(w, xs.max() + 131)]
    elif best_side == "left":
        band = g[:, max(0, xs.min() - 130):xs.min()]
    elif best_side == "bottom":
        band = g[ys.max() + 1:min(h, ys.max() + 131), :]
    else:
        band = g[max(0, ys.min() - 130):ys.min(), :]
    if band.size == 0:
        return 0, 0
    axis = 0 if best_side in ("right", "left") else 1
    prof = band.mean(axis=axis).astype(np.float32)
    prof -= prof.mean()
    ac = np.correlate(prof, prof, "full")[len(prof) - 1:]
    if ac[0] <= 1e-9 or len(ac) < 12:
        return 0, 0
    ac /= ac[0]
    lag = 3 + int(np.argmax(ac[3:12]))
    if ac[lag] < 0.4:
        return 0, 0
    delta = max(lag, round(128 / lag) * lag)
    if best_side == "right":
        return delta, 0
    if best_side == "left":
        return -delta, 0
    if best_side == "bottom":
        return 0, delta
    return 0, -delta


def load_frame(path):
    import cv2
    img = cv2.imread(path, cv2.IMREAD_COLOR)
    if img is None:
        fail(f"cannot read {path}")
    return img


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--strip", required=True)
    ap.add_argument("--masks", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--start", type=int, required=True)
    ap.add_argument("--count", type=int, required=True)
    ap.add_argument("--fps", type=float, default=25.0)
    # --carry DIR: repaired tail frames of the previous chunk, prepended to
    # the input video with zero masks so the diffusion conditions on the
    # previous chunk's actual output instead of drawing fresh texture
    # (cross-chunk drift/flicker fix). Carry outputs are discarded.
    ap.add_argument("--carry", default="")
    args = ap.parse_args()

    home = os.environ.get("DIFFUERASER_HOME", "")
    if not home or not os.path.isdir(home):
        fail("DIFFUERASER_HOME not set or not a directory; sidecar unavailable")
    entry = os.path.join(home, os.environ.get("DIFFUERASER_ENTRY", "run_diffueraser.py"))
    if not os.path.isfile(entry):
        fail(f"{entry} missing; sidecar unavailable")
    try:
        import cv2
        import numpy as np
    except ImportError as e:
        fail(f"import: {e}")

    n = args.count
    frames, masks = [], []
    for k in range(n):
        name = "%05d.png" % (args.start + k)
        frames.append(load_frame(os.path.join(args.strip, name)))
        m = cv2.imread(os.path.join(args.masks, name), cv2.IMREAD_GRAYSCALE)
        if m is None:
            fail(f"cannot read mask {name}")
        masks.append(m)
    h, w = frames[0].shape[:2]

    ncarry = 0
    if args.carry and os.path.isdir(args.carry):
        carry = [load_frame(os.path.join(args.carry, f))
                 for f in sorted(os.listdir(args.carry)) if f.endswith(".png")]
        if carry:
            if carry[0].shape[:2] != (h, w):
                fail("carry frames shape mismatch with strip")
            ncarry = len(carry)
            frames = carry + frames
            masks = [np.zeros((h, w), np.uint8)] * ncarry + masks

    # DiffuEraser rejects inputs shorter than 22 frames; pad by repeating the
    # last frame/mask. Padded outputs are discarded on composite.
    while len(frames) < 22:
        frames.append(frames[-1])
        masks.append(masks[-1])
    n_in = len(frames)

    # Same vertical-context crop as the ProPainter sidecar (R5.2).
    ys = np.zeros(h, dtype=bool)
    for m in masks:
        ys |= m.max(axis=1) > 0
    rows = np.nonzero(ys)[0]
    os.makedirs(args.out, exist_ok=True)
    if len(rows) == 0:
        for k in range(n):
            cv2.imwrite(os.path.join(args.out, "%05d.png" % (args.start + k)), frames[ncarry + k])
        return
    mh = rows[-1] - rows[0] + 1
    pad = int(mh * 1.75 + 0.5)
    y0 = max(0, rows[0] - pad)
    y1 = min(h, rows[-1] + pad + 1)
    # Crop height must be a multiple of 8: DiffuEraser rounds the processing
    # size down to /8 and resamples (474 -> 472), which shifts the repaired
    # patch sub-pixel-wise against the untouched frame at composite time.
    rem = (y1 - y0) % 8
    if rem:
        grow = 8 - rem
        up = min(grow, h - y1)
        y1 += up
        y0 = max(0, y0 - (grow - up))
        if (y1 - y0) % 8:
            y1 -= (y1 - y0) % 8
    # RAFT's correlation pyramid (8x encoder + 3 avg-pool levels) needs the
    # processing height >= 64 px, and the DiffuEraser prior additionally
    # scales the crop by 0.6 -- so sliver masks (a single subtitle row) must
    # be padded to a 112 px crop or RAFT crashes on a zero-size pooling.
    if y1 - y0 < 112:
        cy = (y0 + y1) // 2
        y0 = max(0, cy - 56)
        y1 = min(h, y0 + 112)
        y0 = max(0, y1 - 112)
        if (y1 - y0) % 8:
            y1 -= (y1 - y0) % 8

    work = os.path.join(args.out, ".work-diffueraser")
    shutil.rmtree(work, ignore_errors=True)
    frames_dir_in = os.path.join(work, "frames")
    mask_dir = os.path.join(work, "mask")
    os.makedirs(frames_dir_in, exist_ok=True)
    os.makedirs(mask_dir, exist_ok=True)
    for k in range(n_in):
        cv2.imwrite(os.path.join(frames_dir_in, "%05d.png" % k), frames[k][y0:y1, :])
        mk = np.where(masks[k][y0:y1] > 127, 255, 0).astype(np.uint8)
        cv2.imwrite(os.path.join(mask_dir, "%05d.png" % k), mk)

    fps = "%.6f" % args.fps
    sub_video = os.path.join(work, "chunk.mp4")
    ff = subprocess.run(["ffmpeg", "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
                         "-framerate", fps, "-i", os.path.join(frames_dir_in, "%05d.png"),
                         "-c:v", "libx264rgb", "-qp", "0", "-r", fps, sub_video],
                        capture_output=True, text=True)
    if ff.returncode != 0:
        fail(f"ffmpeg: {ff.stderr[-300:]}")
    mask_video = os.path.join(work, "mask.mp4")
    ff = subprocess.run(["ffmpeg", "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
                         "-framerate", fps, "-i", os.path.join(mask_dir, "%05d.png"),
                         "-c:v", "libx264", "-qp", "0", "-pix_fmt", "yuv420p",
                         "-r", fps, mask_video],
                        capture_output=True, text=True)
    if ff.returncode != 0:
        fail(f"ffmpeg mask: {ff.stderr[-300:]}")

    result_dir = os.path.join(work, "results")
    os.makedirs(result_dir, exist_ok=True)
    cmd = [sys.executable, entry,
           "--input_video", sub_video,
           "--input_mask", mask_video,
           "--save_path", result_dir]
    extra = os.environ.get("DIFFUERASER_EXTRA_ARGS", "")
    if extra:
        cmd += extra.split()
    # Our masks are already dilated upstream (tight-dilate + mask-dilation);
    # DiffuEraser's default dilation 8 is calibrated for raw stroke masks and
    # would over-erode context (waxy fills, glass/rim loss).
    if "--mask_dilation_iter" not in extra:
        cmd += ["--mask_dilation_iter", "2"]
    r = subprocess.run(cmd, cwd=home, capture_output=True, text=True)
    if r.returncode != 0:
        fail(f"inference exit {r.returncode}: {(r.stderr or r.stdout)[-400:]}")
    produced_video = None
    for root, _dirs, files in os.walk(result_dir):
        mp4s = [f for f in files if f.endswith(".mp4")]
        if mp4s:
            produced_video = os.path.join(root, sorted(mp4s)[0])
            break
    if produced_video is None:
        fail("inference produced no mp4; if upstream ENTRY has no argparse, "
             "add --input_video/--input_mask/--output_dir or set DIFFUERASER_ENTRY")

    out_dir = os.path.join(work, "out_frames")
    os.makedirs(out_dir, exist_ok=True)
    ff = subprocess.run(["ffmpeg", "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
                         "-i", produced_video, "-start_number", "0",
                         os.path.join(out_dir, "%05d.png")],
                        capture_output=True, text=True)
    if ff.returncode != 0:
        fail(f"ffmpeg decode: {ff.stderr[-300:]}")
    produced = sorted(f for f in os.listdir(out_dir) if f.endswith(".png"))
    if len(produced) < ncarry + n:
        fail(f"result has {len(produced)} frames, want {ncarry + n}")

    # Composite the diffusion crop back. Poisson (seamlessClone NORMAL_CLONE)
    # is the default: regenerated high-frequency texture (shirt stripes) never
    # phase-matches the original at the mask edge, and any alpha ramp leaves a
    # visible cut or soft band there. Poisson keeps diffusion gradients inside
    # the mask while pinning boundary pixels to the original frame, absorbing
    # the mismatch as a smooth low-frequency correction. DIFFUERASER_BLEND=
    # feather selects the old Gaussian-ramp composite as a fallback.
    # Diffusion output is also measurably softer than the source texture
    # (stripe contrast ~15 vs ~19), so the masked area gets a mild unsharp
    # boost before cloning (DIFFUERASER_SHARPEN, 0 disables; default 1.0
    # measured crisper on pinstripe texture with no halo regression vs 0.6).
    # Poisson pins boundary values but cannot phase-align periodic texture:
    # regenerated pinstripes keep a random phase, so stripes break at the
    # mask edge even after cloning. When the texture just outside the mask
    # is strongly periodic, we additionally re-clone from the SAME source
    # frame translated by an integer number of stripe periods (boundary
    # phase matches by construction), and adopt that clone only where its
    # low-frequency colour agrees with the diffusion composite — pinstripe
    # cloth passes the gate, skin/glass/plain cloth keeps the diffusion
    # pixels (DIFFUERASER_STRIPES, 0 disables). The shift is estimated once
    # per chunk so frames cannot drift against each other.
    blend = os.environ.get("DIFFUERASER_BLEND", "poisson")
    feather = max(0, int(os.environ.get("PROPAINTER_FEATHER", "6")))
    sharpen = float(os.environ.get("DIFFUERASER_SHARPEN", "1.0"))
    crop_h = y1 - y0
    sp_dx = sp_dy = 0
    if os.environ.get("DIFFUERASER_STRIPES", "1") != "0" and n > 0:
        k0 = ncarry + n // 2
        mk0 = np.where(masks[k0][y0:y1, :] > 127, 255, 0).astype(np.uint8)
        sp_dx, sp_dy = _stripe_shift(frames[k0][y0:y1, :], mk0)
    for k in range(n):
        sub = load_frame(os.path.join(out_dir, produced[ncarry + k]))
        if sub.shape[0] != crop_h or sub.shape[1] != w:
            sub = cv2.resize(sub, (w, crop_h), interpolation=cv2.INTER_LINEAR)
        base = frames[ncarry + k][y0:y1, :]
        mk = np.where(masks[ncarry + k][y0:y1, :] > 127, 255, 0).astype(np.uint8)
        if sharpen > 0 and mk.any():
            blur = cv2.GaussianBlur(sub, (0, 0), 1.2)
            detail = sub.astype(np.float32) - blur.astype(np.float32)
            a_s = (cv2.GaussianBlur(mk.astype(np.float32), (9, 9), 0)
                   / 255.0)[..., None]
            sub = np.clip(sub.astype(np.float32) + sharpen * detail * a_s,
                          0, 255).astype(np.uint8)
        merged = None
        if blend == "poisson" and mk.any():
            ys, xs = np.nonzero(mk)
            center = (int((xs.min() + xs.max()) // 2),
                      int((ys.min() + ys.max()) // 2))
            try:
                merged = cv2.seamlessClone(sub, base, mk, center,
                                           cv2.NORMAL_CLONE)
            except cv2.error:
                merged = None
        if merged is None:
            a = mk.astype(np.float32) / 255.0
            if feather > 0:
                kf = max(3, feather // 2 * 2 + 1)
                a = cv2.GaussianBlur(a, (kf, kf), 0)
                peak = a.max()
                if peak > 0:
                    a /= peak
            a = a[..., None]
            merged = (base.astype(np.float32) * (1 - a)
                      + sub.astype(np.float32) * a).astype(np.uint8)
        if (sp_dx or sp_dy) and mk.any():
            # the shift source must be subtitle-free: the raw base carries the
            # white glyphs, and translating them into the stripe zone passes
            # the low-frequency gate (white ≈ bright stripe). Erase the stroke
            # pixels first; Telea extends the surrounding texture into the
            # glyph cells, which is exactly the phase-correct content the
            # re-clone should sample.
            hsv = cv2.cvtColor(base, cv2.COLOR_BGR2HSV)
            white = (((hsv[:, :, 1] < 80) & (hsv[:, :, 2] > 190))
                     & (mk > 0)).astype(np.uint8) * 255
            stroke = cv2.dilate(white, np.ones((9, 9), np.uint8))
            clean = (cv2.inpaint(base, stroke, 6, cv2.INPAINT_TELEA)
                     if stroke.any() else base)
            m_sh = np.float32([[1, 0, sp_dx], [0, 1, sp_dy]])
            shifted = cv2.warpAffine(clean, m_sh, (w, crop_h),
                                     flags=cv2.INTER_LINEAR,
                                     borderMode=cv2.BORDER_REPLICATE)
            ys, xs = np.nonzero(mk)
            center = (int((xs.min() + xs.max()) // 2),
                      int((ys.min() + ys.max()) // 2))
            try:
                clone2 = cv2.seamlessClone(shifted, merged, mk, center,
                                           cv2.NORMAL_CLONE)
            except cv2.error:
                clone2 = None
            if clone2 is not None:
                lc = cv2.GaussianBlur(
                    cv2.cvtColor(clone2, cv2.COLOR_BGR2Lab),
                    (0, 0), 4.0).astype(np.float32)
                lm = cv2.GaussianBlur(
                    cv2.cvtColor(merged, cv2.COLOR_BGR2Lab),
                    (0, 0), 4.0).astype(np.float32)
                dist = np.linalg.norm(lc - lm, axis=2)
                alpha = np.exp(-(dist / 12.0) ** 2)
                alpha[mk == 0] = 0
                alpha = cv2.GaussianBlur(alpha, (0, 0), 3.0)
                am = alpha.max()
                if am > 0:
                    alpha /= am
                merged = (merged.astype(np.float32) * (1 - alpha[..., None])
                          + clone2.astype(np.float32)
                          * alpha[..., None]).astype(np.uint8)
        out = frames[ncarry + k].copy()
        out[y0:y1, :] = merged
        cv2.imwrite(os.path.join(args.out, "%05d.png" % (args.start + k)), out)


if __name__ == "__main__":
    main()
