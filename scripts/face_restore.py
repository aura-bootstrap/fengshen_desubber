#!/usr/bin/env python3
"""Face-prior sidecar for desub: restores faces damaged by inpainting with
GFPGAN, gated to the repair zone so untouched content never changes.

  --strip DIR   repaired RGB band frames, %05d.png (modified IN PLACE)
  --masks DIR   gray repair masks (255 = was inpainted), same numbering
  --start N     first frame index (relative to the job, 0-based)
  --count M     number of frames to process

Only faces whose full bounding box lies inside the band AND intersects the
dilated repair mask are restored; the restored pixels blend back inside
(face box ∩ dilated mask) with a soft edge. Faces outside the repair zone —
and partial faces cut by the band edge — pass through unchanged.

Env knobs: GFPGAN_WEIGHTS (dir with GFPGANv1.4.pth,
detection_ResNet50_Final.pth, parsing_bisenet.pth; required),
FACE_GATE_DILATE (px, default 25), FACE_MIN_SIZE (px, default 24).
"""
import argparse
import os
import sys


def fail(msg):
    print(f"facerestore: {msg}", file=sys.stderr)
    sys.exit(3)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--strip", required=True)
    ap.add_argument("--masks", required=True)
    ap.add_argument("--start", type=int, required=True)
    ap.add_argument("--count", type=int, required=True)
    args = ap.parse_args()

    wdir = os.environ.get("GFPGAN_WEIGHTS", "")
    if not wdir or not os.path.isdir(wdir):
        fail("GFPGAN_WEIGHTS not set or not a directory; sidecar unavailable")
    model_path = os.path.join(wdir, "GFPGANv1.4.pth")
    if not os.path.isfile(model_path):
        fail(f"{model_path} missing")
    # facexlib resolves helper weights under <cwd>/gfpgan/weights with its own
    # file names (and would otherwise download them); stage ours there.
    stage = os.path.join(os.getcwd(), "gfpgan", "weights")
    os.makedirs(stage, exist_ok=True)
    for src, dst in (("detection_ResNet50_Final.pth", "detection_Resnet50_Final.pth"),
                     ("parsing_bisenet.pth", "parsing_bisenet.pth"),
                     ("parsing_parsenet.pth", "parsing_parsenet.pth")):
        s, d = os.path.join(wdir, src), os.path.join(stage, dst)
        if os.path.isfile(s) and not os.path.isfile(d):
            try:
                os.link(s, d)
            except OSError:
                import shutil as _sh
                _sh.copy(s, d)
    try:
        import cv2
        import numpy as np
        import torch
    except ImportError as e:
        fail(f"import: {e}")
    # basicsr 1.4.2 imports torchvision.transforms.functional_tensor, removed
    # in torchvision 0.16+; shim it onto the current namespace.
    try:
        import torchvision.transforms.functional as _tvf
        import types
        _shim = types.ModuleType("torchvision.transforms.functional_tensor")
        _shim.rgb_to_grayscale = _tvf.rgb_to_grayscale
        sys.modules.setdefault("torchvision.transforms.functional_tensor", _shim)
    except ImportError:
        pass
    try:
        from gfpgan import GFPGANer
        from facexlib.detection import init_detection_model
    except ImportError as e:
        fail(f"gfpgan import: {e}")

    device = "cuda" if torch.cuda.is_available() else "cpu"
    restorer = GFPGANer(model_path=model_path, upscale=1, arch="clean",
                        channel_multiplier=2, bg_upsampler=None, device=device)
    det = init_detection_model("retinaface_resnet50", half=False, device=device)

    gate_d = int(os.environ.get("FACE_GATE_DILATE", "25"))
    min_sz = int(os.environ.get("FACE_MIN_SIZE", "24"))
    gk = np.ones((gate_d * 2 + 1,) * 2, np.uint8)

    n = args.count
    restored_ct = 0
    for k in range(n):
        name = "%05d.png" % (args.start + k)
        fp = os.path.join(args.strip, name)
        frame = cv2.imread(fp, cv2.IMREAD_COLOR)
        if frame is None:
            fail(f"cannot read {name}")
        mk = cv2.imread(os.path.join(args.masks, name), cv2.IMREAD_GRAYSCALE)
        if mk is None:
            fail(f"cannot read mask {name}")
        if not mk.any():
            continue
        h, w = frame.shape[:2]
        gate = cv2.dilate((mk > 127).astype(np.uint8) * 255, gk) > 0

        with torch.no_grad():
            bboxes = det.detect_faces(frame, 0.5)
        keep = []
        for b in bboxes:
            x0, y0, x1, y1 = (int(round(v)) for v in b[:4])
            if x0 < 0 or y0 < 0 or x1 > w or y1 > h:
                continue  # partial face cut by the band edge
            if x1 - x0 < min_sz or y1 - y0 < min_sz:
                continue
            if not gate[y0:y1, x0:x1].any():
                continue  # face does not intersect the repair zone
            keep.append((x0, y0, x1, y1))
        if not keep:
            continue

        with torch.no_grad():
            _, _, restored = restorer.enhance(frame, has_aligned=False,
                                              only_center_face=False,
                                              paste_back=True)
        if restored is None:
            continue
        zone = np.zeros((h, w), np.uint8)
        for (x0, y0, x1, y1) in keep:
            zone[y0:y1, x0:x1] = 255
        soft = cv2.GaussianBlur(
            ((zone > 0) & gate).astype(np.float32), (0, 0), 3.0)[..., None]
        # Confine the blend to the gate (plus a 4px skirt): the Gaussian tail
        # must never leak the restoration into untouched content.
        confine = cv2.dilate(gate.astype(np.uint8), np.ones((9, 9), np.uint8)) > 0
        soft[~confine] = 0
        if soft.max() <= 0:
            continue
        out = (frame.astype(np.float32) * (1 - soft)
               + restored.astype(np.float32) * soft)
        cv2.imwrite(fp, out.clip(0, 255).astype(np.uint8))
        restored_ct += 1
    print(f"facerestore: {restored_ct}/{n} frames restored", file=sys.stderr)


if __name__ == "__main__":
    main()
