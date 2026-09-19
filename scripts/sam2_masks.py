#!/usr/bin/env python3
"""SAM2 mask-refinement sidecar for desub: upgrades dilated stroke masks to
pixel-level segmentation propagated over the shot, same PNG-directory
contract as propainter_infer.py so the Go caller can export once and reuse.

  --strip DIR   RGB band frames, %05d.png numbered from 0 over the whole job
  --masks DIR   gray masks (255 = inpaint), same numbering
  --out DIR     refined masks for the requested range, same numbering
  --start N     first frame index (relative to the job, 0-based)
  --count M     number of frames to process (never cross a shot cut)
  --gate R      px dilation of the ORIGINAL masks restricting refinement
                (default 12): SAM2 output is intersected with this gate so
                segmentation cannot leak beyond the stroke neighbourhood

SAM2 itself is NOT bundled: set SAM2_HOME to a checkout of
https://github.com/facebookresearch/sam2 (pip install -e .) with weights at
checkpoints/. Env knobs: SAM2_HOME (required), SAM2_CKPT (default
checkpoints/sam2.1_hiera_large.pt), SAM2_CONFIG (default
configs/sam2.1/sam2.1_hiera_l.yaml), SAM2_DEVICE (default cuda).
"""
import argparse
import os
import shutil
import subprocess
import sys


def fail(msg):
    print(f"sam2: {msg}", file=sys.stderr)
    sys.exit(3)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--strip", required=True)
    ap.add_argument("--masks", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--start", type=int, required=True)
    ap.add_argument("--count", type=int, required=True)
    ap.add_argument("--gate", type=int, default=12)
    args = ap.parse_args()

    home = os.environ.get("SAM2_HOME", "")
    if not home or not os.path.isdir(home):
        fail("SAM2_HOME not set or not a directory; sidecar unavailable")
    ckpt = os.environ.get("SAM2_CKPT", "checkpoints/sam2.1_hiera_large.pt")
    if not os.path.isabs(ckpt):
        ckpt = os.path.join(home, ckpt)
    config = os.environ.get("SAM2_CONFIG", "configs/sam2.1/sam2.1_hiera_l.yaml")
    if not os.path.isfile(ckpt):
        fail(f"{ckpt} missing")
    try:
        import cv2
        import numpy as np
    except ImportError as e:
        fail(f"import: {e}")

    n = args.count
    frames, masks = [], []
    for k in range(n):
        name = "%05d.png" % (args.start + k)
        f = cv2.imread(os.path.join(args.strip, name), cv2.IMREAD_COLOR)
        if f is None:
            fail(f"cannot read {name}")
        m = cv2.imread(os.path.join(args.masks, name), cv2.IMREAD_GRAYSCALE)
        if m is None:
            fail(f"cannot read mask {name}")
        frames.append(f)
        masks.append(m)
    h, w = frames[0].shape[:2]
    os.makedirs(args.out, exist_ok=True)

    def write(k, mk):
        cv2.imwrite(os.path.join(args.out, "%05d.png" % (args.start + k)), mk)

    gate = np.ones((args.gate * 2 + 1,) * 2, np.uint8)
    text_frames = [k for k in range(n) if masks[k].any()]
    if not text_frames:
        for k in range(n):
            write(k, np.zeros((h, w), np.uint8))
        return

    # Prompt on the frame with the largest masked area (subtitle fully on
    # screen); each connected component becomes one tracked object.
    kf = max(text_frames, key=lambda k: int(cv2.countNonZero(masks[k])))
    nlab, labels, stats, _ = cv2.connectedComponentsWithStats(
        (masks[kf] > 127).astype(np.uint8), connectivity=8)
    objs = [(lab, stats[lab, cv2.CC_STAT_AREA])
            for lab in range(1, nlab) if stats[lab, cv2.CC_STAT_AREA] >= 8]
    objs.sort(key=lambda t: -t[1])
    objs = objs[:16]
    if not objs:
        for k in range(n):
            write(k, masks[k])
        return

    work = os.path.join(args.out, ".work-sam2")
    shutil.rmtree(work, ignore_errors=True)
    vid_dir = os.path.join(work, "frames")
    os.makedirs(vid_dir, exist_ok=True)
    # SAM2 wants a contiguous directory; re-number our range from 0.
    for k in range(n):
        cv2.imwrite(os.path.join(vid_dir, "%05d.jpg" % k), frames[k],
                    [cv2.IMWRITE_JPEG_QUALITY, 95])

    sys.path.insert(0, home)
    try:
        import torch
        from sam2.build_sam import build_sam2_video_predictor
    except ImportError as e:
        fail(f"sam2 import: {e}")
    device = os.environ.get("SAM2_DEVICE", "cuda" if torch.cuda.is_available() else "cpu")
    predictor = build_sam2_video_predictor(config, ckpt, device=device)
    state = predictor.init_state(video_path=vid_dir)
    for j, (lab, _area) in enumerate(objs):
        predictor.add_new_mask(state, frame_idx=kf, obj_id=j + 1,
                               mask=(labels == lab))

    refined = {}
    with torch.inference_mode():
        for fidx, _obj_ids, logits in predictor.propagate_in_video(state):
            merged = (logits > 0).any(dim=0)[0].cpu().numpy()
            refined[fidx] = merged

    for k in range(n):
        if k not in refined or not masks[k].any():
            write(k, np.zeros((h, w), np.uint8))
            continue
        seg = (refined[k].astype(np.uint8)) * 255
        keep = cv2.dilate((masks[k] > 127).astype(np.uint8) * 255, gate) > 0
        out = np.where((seg > 0) & keep, 255, 0).astype(np.uint8)
        # Segmentation can legitimately miss thin strokes; never shrink below
        # the original mask.
        out[masks[k] > 127] = 255
        write(k, out)


if __name__ == "__main__":
    main()
