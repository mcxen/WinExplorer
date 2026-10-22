#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Bundled file extractor for the Sunvale assistant.

Reads one file and prints a JSON result on stdout:

    {
      "ok": true, "method": "Pillow", "text": "...", "truncated": false,
      "meta": {"duration": 0, "width": 0, "height": 0,
               "video_codec": "", "audio_codec": "", "pages": 0},
      "frames": [{"label": "00:01", "mime": "image/jpeg",
                  "data": "<base64>", "width": 0, "height": 0}],
      "warnings": ["..."]
    }

Only the standard library is required. Extra modules are used when they are
installed, one at a time: Pillow (photos, camera formats), OpenCV (video
frames), pypdf / pdfminer (PDF text), python-docx / openpyxl / python-pptx
(office), mutagen (audio tags), numpy + matplotlib (audio spectrogram).
A machine with more of them reads more formats; nothing here fails because a
module is missing.

Usage: extract.py <path> [--kind KIND] [--frames N] [--side PX]
"""

import argparse
import base64
import io
import json
import os
import re
import struct
import subprocess
import sys
import zipfile
import xml.etree.ElementTree as ET

WARNINGS = []


def warn(msg):
    if msg and msg not in WARNINGS:
        WARNINGS.append(str(msg))


def b64(data):
    return base64.b64encode(data).decode("ascii")


def ok_json(text, method, frames=None, meta=None, truncated=False):
    out = {
        "ok": True,
        "method": method,
        "text": text or "",
        "truncated": bool(truncated),
        "meta": meta or {},
        "frames": frames or [],
        "warnings": WARNINGS,
    }
    json.dump(out, sys.stdout, ensure_ascii=False)
    return 0


def fail(msg):
    warn(msg)
    json.dump({"ok": False, "text": "", "warnings": WARNINGS}, sys.stdout, ensure_ascii=False)
    return 1


# --- office: zip + XML, stdlib only -----------------------------------------

NS_STRIP = re.compile(r"\{[^}]*\}")


def _xml_text(raw, breaks=("p", "h", "br", "row", "tr", "li", "tc")):
    """Character data out of an OOXML part, breaking on paragraph-ish ends."""
    out = []
    try:
        root = ET.fromstring(raw)
    except Exception:
        return ""
    for node in root.iter():
        tag = NS_STRIP.sub("", node.tag or "")
        if tag in ("t", "v") and node.text:
            out.append(node.text)
        if tag in breaks:
            out.append("\n")
    return re.sub(r"\n{3,}", "\n\n", "".join(out)).strip()


def office_text(path, max_bytes=2_000_000):
    """docx/xlsx/pptx/odt as text through their XML parts."""
    parts = []
    with zipfile.ZipFile(path) as z:
        names = [n for n in z.namelist() if n.endswith(".xml")]
        if not names:
            return ""
        doc_parts = [
            n for n in names
            if re.search(r"(document|sheet|slide|content)\d*\.xml$", n, re.I)
        ]
        for name in sorted(doc_parts)[:60]:
            if sum(len(p) for p in parts) > max_bytes:
                break
            with z.open(name) as f:
                parts.append(_xml_text(f.read(max_bytes)))
    return "\n".join(p for p in parts if p).strip()


# --- pdf --------------------------------------------------------------------

def pdf_text(path):
    try:
        from pypdf import PdfReader  # type: ignore
    except Exception:
        try:
            from PyPDF2 import PdfReader  # type: ignore
        except Exception:
            return "", 0
    try:
        reader = PdfReader(path)
        pages = len(getattr(reader, "pages", []) or [])
        chunks = []
        for page in getattr(reader, "pages", [])[:80]:
            try:
                chunks.append(page.extract_text() or "")
            except Exception:
                continue
        return "\n".join(chunks).strip(), pages
    except Exception as exc:
        warn("pypdf 读取失败: %s" % exc)
        return "", 0


# --- images -----------------------------------------------------------------

def image_frames(path, side, frames):
    """Decode a photo and return it re-encoded, plus its pixel size."""
    try:
        from PIL import Image  # type: ignore
    except Exception:
        warn("未安装 Pillow，图片读取依赖 Go 内置解码")
        return [], {}
    try:
        with Image.open(path) as im:
            im.load()
            w, h = im.size
            frames_out = []
            n = 1
            if getattr(im, "is_animated", False):
                n = min(int(frames or 1), 4)
            for i in range(n):
                if i:
                    im.seek(i)
                frames_out.append(encode_pil(im, side, "第 %d 帧" % (i + 1)))
            return frames_out, {"width": w, "height": h}
    except Exception as exc:
        warn("图片解码失败: %s" % exc)
        return [], {}


def encode_pil(im, side, label):
    from PIL import Image  # type: ignore
    frame = im.convert("RGBA") if im.mode in ("RGBA", "LA", "P") else im.convert("RGB")
    w, h = frame.size
    if max(w, h) > side:
        scale = side / float(max(w, h))
        frame = frame.resize((max(1, int(w * scale)), max(1, int(h * scale))), Image.LANCZOS)
    buf = io.BytesIO()
    mime = "image/png"
    if frame.mode == "RGB":
        mime = "image/jpeg"
        frame.save(buf, "JPEG", quality=82)
    else:
        frame.save(buf, "PNG")
    return {
        "label": label,
        "mime": mime,
        "data": b64(buf.getvalue()),
        "width": frame.size[0],
        "height": frame.size[1],
    }


# --- video ------------------------------------------------------------------

def video_frames(path, side, count):
    """Frames via OpenCV, or via the ffmpeg binary when it is installed."""
    meta = {}
    try:
        import cv2  # type: ignore
    except Exception:
        cv2 = None
    if cv2 is not None:
        try:
            cap = cv2.VideoCapture(path)
            total = int(cap.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
            fps = float(cap.get(cv2.CAP_PROP_FPS) or 0)
            w = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH) or 0)
            h = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT) or 0)
            duration = total / fps if fps > 0 else 0
            meta = {"width": w, "height": h, "duration": duration}
            out = []
            for i in range(int(count or 3)):
                pos = 0.1 + 0.8 * (i / float(max(int(count or 3) - 1, 1)))
                cap.set(cv2.CAP_PROP_POS_FRAMES, int(total * pos) if total else 0)
                ok, frame = cap.read()
                if not ok:
                    continue
                ok, buf = cv2.imencode(".jpg", frame, [int(cv2.IMWRITE_JPEG_QUALITY), 82])
                if not ok:
                    continue
                out.append({
                    "label": "%.0f 秒" % (duration * pos) if duration else "第 %d 帧" % (i + 1),
                    "mime": "image/jpeg",
                    "data": b64(buf.tobytes()),
                    "width": frame.shape[1],
                    "height": frame.shape[0],
                })
            cap.release()
            if out:
                return out, meta
        except Exception as exc:
            warn("OpenCV 读取视频失败: %s" % exc)
    return ffmpeg_frames(path, side, count), meta


def ffmpeg_frames(path, side, count):
    import shutil
    import tempfile
    if not shutil.which("ffmpeg"):
        return []
    probe = media_meta(path)
    duration = float(probe.get("duration") or 0)
    out = []
    with tempfile.TemporaryDirectory() as tmp:
        for i in range(int(count or 3)):
            pos = duration * (0.1 + 0.8 * (i / float(max(int(count or 3) - 1, 1)))) if duration > 1 else i
            dst = os.path.join(tmp, "f%d.jpg" % i)
            cmd = ["ffmpeg", "-y", "-ss", "%.3f" % max(pos, 0), "-i", path,
                   "-frames:v", "1", "-update", "1",
                   "-vf", "scale='min(%d,iw)':-1" % int(side or 1024), dst]
            try:
                subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, timeout=30)
            except Exception:
                continue
            if os.path.exists(dst):
                with open(dst, "rb") as f:
                    data = f.read()
                if data:
                    out.append({
                        "label": "%.0f 秒" % pos if duration else "第 %d 帧" % (i + 1),
                        "mime": "image/jpeg",
                        "data": b64(data),
                        "width": 0, "height": 0,
                    })
    return out


# --- audio ------------------------------------------------------------------

def audio_meta(path):
    try:
        import mutagen  # type: ignore
        info = mutagen.File(path)
        if info is not None:
            return {"duration": float(getattr(info.info, "length", 0) or 0)}
    except Exception:
        pass
    if path.lower().endswith(".wav"):
        try:
            import wave
            with wave.open(path, "rb") as w:
                rate = w.getframerate() or 1
                return {"duration": w.getnframes() / float(rate)}
        except Exception:
            pass
    return media_meta(path)


def audio_spectrogram(path, side):
    """A picture of the sound, so a vision model can judge pace and silence."""
    try:
        import numpy as np  # type: ignore
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt  # type: ignore
    except Exception:
        warn("未安装 numpy/matplotlib，音频没有声谱图")
        return []
    try:
        import wave
        with wave.open(path, "rb") as w:
            rate = w.getframerate() or 1
            frames = w.readframes(min(w.getnframes(), rate * 60))
            data = np.frombuffer(frames, dtype=np.int16).astype(float) / 32768.0
            if w.getnchannels() > 1:
                data = data.reshape(-1, w.getnchannels()).mean(axis=1)
    except Exception as exc:
        warn("音频读取失败: %s" % exc)
        return []
    if data.size == 0:
        return []
    fig = plt.figure(figsize=(side / 100.0, side / 200.0), dpi=100)
    ax = fig.add_subplot(111)
    ax.specgram(data, Fs=rate, NFFT=1024, noverlap=512, cmap="magma")
    ax.axis("off")
    buf = io.BytesIO()
    fig.savefig(buf, format="png", bbox_inches="tight", pad_inches=0)
    plt.close(fig)
    return [{
        "label": "声谱图",
        "mime": "image/png",
        "data": b64(buf.getvalue()),
        "width": 0, "height": 0,
    }]


def media_meta(path):
    """Duration and codecs through ffprobe, when the machine has it."""
    import shutil
    import tempfile
    if not shutil.which("ffprobe"):
        return {}
    try:
        out = subprocess.run(
            ["ffprobe", "-v", "error", "-print_format", "json",
             "-show_format", "-show_streams", path],
            check=True, capture_output=True, timeout=15).stdout
        payload = json.loads(out.decode("utf-8", "replace"))
    except Exception:
        return {}
    meta = {}
    try:
        meta["duration"] = float(payload.get("format", {}).get("duration") or 0)
    except Exception:
        pass
    for stream in payload.get("streams", []):
        if stream.get("codec_type") == "video" and not meta.get("width"):
            meta["width"] = int(stream.get("width") or 0)
            meta["height"] = int(stream.get("height") or 0)
            meta["video_codec"] = str(stream.get("codec_name") or "").upper()
        if stream.get("codec_type") == "audio" and not meta.get("audio_codec"):
            meta["audio_codec"] = str(stream.get("codec_name") or "").upper()
    del tempfile
    return meta


# --- dispatch ---------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("path")
    ap.add_argument("--kind", default="")
    ap.add_argument("--frames", type=int, default=4)
    ap.add_argument("--side", type=int, default=1024)
    args = ap.parse_args()
    path = args.path
    if not os.path.isfile(path):
        return fail("文件不存在: %s" % path)
    kind = (args.kind or "").lower()
    low = path.lower()
    try:
        if kind in ("text", "code") or _ext(low) in ("txt", "md", "log", "csv", "json", "xml"):
            return ok_json(read_text(path), "Python 文本读取")
        if kind == "image" or _ext(low) in ("heic", "heif", "tiff", "tif", "avif", "webp", "bmp", "ico"):
            frames, meta = image_frames(path, args.side, args.frames)
            if frames:
                return ok_json("图片画面见附图。", "Pillow", frames, meta)
            return fail("没有可用的图片解码器")
        if kind == "video" or _ext(low) in ("mp4", "mov", "mkv", "avi", "webm", "m4v", "wmv", "flv"):
            frames, meta = video_frames(path, args.side, args.frames)
            meta.update(media_meta(path))
            return ok_json("视频画面见附图。", "OpenCV/ffmpeg", frames, meta)
        if kind == "audio" or _ext(low) in ("mp3", "m4a", "aac", "flac", "ogg", "opus", "wav", "aiff"):
            frames = audio_spectrogram(path, args.side)
            meta = audio_meta(path)
            return ok_json("音频声谱图见附图。", "numpy/matplotlib", frames, meta)
        if _ext(low) == "pdf" or kind == "doc":
            text, pages = pdf_text(path)
            meta = {"pages": pages} if pages else {}
            if text:
                return ok_json(text, "pypdf", None, meta)
            frames, _ = image_frames(path, args.side, 1)
            return ok_json("", "Pillow", frames, meta)
        # Documents and anything else: office XML walk beats giving up.
        if zipfile.is_zipfile(path):
            text = office_text(path)
            if text:
                return ok_json(text, "zip+XML", None, meta=None)
        return fail("这个格式脚本也读不了: %s" % _ext(low))
    except Exception as exc:  # never let a traceback be the output
        return fail("脚本异常: %s" % exc)


def read_text(path, limit=200_000):
    with open(path, "rb") as f:
        raw = f.read(limit)
    if raw.startswith(b"\xff\xfe") or raw.startswith(b"\xfe\xff"):
        return raw.decode("utf-16", "replace")
    return raw.decode("utf-8", "replace")


def _ext(low):
    return os.path.splitext(low)[1].lstrip(".")


if __name__ == "__main__":
    sys.exit(main())
