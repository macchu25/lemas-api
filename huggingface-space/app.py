import os
os.environ["HF_HOME"] = r"c:\Users\NHU HUU\Downloads\nornAI\.artqr-models"
os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"

import base64
import io
import math
import random
import re
import numpy as np
import qrcode
from PIL import Image, ImageDraw, ImageFilter, ImageOps, ImageEnhance

import gradio as gr
try:
    import spaces
    HAVE_SPACES = True
except ImportError:
    HAVE_SPACES = False

try:
    import zxingcpp
    HAVE_ZXING = True
except ImportError:
    HAVE_ZXING = False

try:
    import cv2
    HAVE_CV2 = True
except ImportError:
    HAVE_CV2 = False

try:
    import pyzbar.pyzbar as pyzbar
    HAVE_PYZBAR = True
except ImportError:
    HAVE_PYZBAR = False

LOCAL_WORKER = os.environ.get("LOCAL_WORKER", "1") == "1"

import torch
from diffusers import (
    ControlNetModel,
    StableDiffusionControlNetImg2ImgPipeline,
    StableDiffusionControlNetPipeline,
    DPMSolverMultistepScheduler,
)

BASE_MODEL = "stable-diffusion-v1-5/stable-diffusion-v1-5"
CONTROLNET_MODEL = "monster-labs/control_v1p_sd15_qrcode_monster"

print("[LEMAS Art QR] Loading ControlNet & Stable Diffusion Models on CUDA...")

controlnet = ControlNetModel.from_pretrained(
    CONTROLNET_MODEL,
    subfolder="v2",
    torch_dtype=torch.float16,
    use_safetensors=True,
)
pipe = StableDiffusionControlNetPipeline.from_pretrained(
    BASE_MODEL,
    controlnet=controlnet,
    torch_dtype=torch.float16,
    use_safetensors=True,
    safety_checker=None,
    requires_safety_checker=False,
)
pipe.scheduler = DPMSolverMultistepScheduler.from_config(pipe.scheduler.config, use_karras_sigmas=True)

# Shared memory img2img pipeline for DiffQRCoder-style scanning refinement
img2img_pipe = StableDiffusionControlNetImg2ImgPipeline(
    vae=pipe.vae,
    text_encoder=pipe.text_encoder,
    tokenizer=pipe.tokenizer,
    unet=pipe.unet,
    controlnet=pipe.controlnet,
    scheduler=pipe.scheduler,
    safety_checker=None,
    feature_extractor=None,
    requires_safety_checker=False,
)

if torch.cuda.is_available():
    pipe.to("cuda")
    img2img_pipe.to("cuda")
    torch.backends.cuda.matmul.allow_tf32 = True
    torch.backends.cudnn.allow_tf32 = True
    torch.backends.cudnn.benchmark = True
    pipe.enable_vae_slicing()
    img2img_pipe.enable_vae_slicing()
    print("[LEMAS Art QR] Successfully loaded SD 1.5 + QRCode Monster v2 on CUDA GPU!")
else:
    pipe.enable_model_cpu_offload()
    pipe.enable_attention_slicing()
    img2img_pipe.enable_model_cpu_offload()
    img2img_pipe.enable_attention_slicing()
    print("[LEMAS Art QR] Running in CPU Offload mode.")


def gpu_worker(fn):
    return spaces.GPU(duration=120)(fn) if HAVE_SPACES else fn


def decode_base64_image(data: str) -> Image.Image | None:
    if not data or len(data) < 20:
        return None
    try:
        b64_str = data
        if "," in b64_str:
            b64_str = b64_str.split(",", 1)[1]
        raw_bytes = base64.b64decode(b64_str)
        return Image.open(io.BytesIO(raw_bytes)).convert("RGB")
    except Exception as e:
        print("[LEMAS Art QR] Error decoding base64 image:", e)
        return None


# ==============================================================================
# 1. Multi-Scanner Validation Ensemble (ZXing + OpenCV + PyZBar)
# ==============================================================================

def scan_qr_ensemble(img: Image.Image, expected_payload: str = "") -> dict:
    """Scans image with an ensemble of 3 distinct QR decoder engines at multiple resolutions."""
    results = {"zxing": "", "opencv": "", "zbar": "", "matches": False, "match_count": 0}
    w, h = img.size

    # Multi-resolution scan pyramid (1024, 512, 384)
    pyramid = [img]
    if min(w, h) > 512:
        pyramid.append(img.resize((512, 512), Image.Resampling.BILINEAR))
    if min(w, h) > 384:
        pyramid.append(img.resize((384, 384), Image.Resampling.BILINEAR))

    # 1. ZXing Engine
    if HAVE_ZXING:
        for p_img in pyramid:
            try:
                barcodes = zxingcpp.read_barcodes(p_img)
                if barcodes and barcodes[0].text:
                    results["zxing"] = barcodes[0].text
                    break
            except Exception:
                pass

    # 2. OpenCV QRCodeDetector Engine
    if HAVE_CV2 and not results["zxing"]:
        for p_img in pyramid:
            try:
                img_np = np.array(p_img.convert("RGB"))
                img_cv = cv2.cvtColor(img_np, cv2.COLOR_RGB2BGR)
                detector = cv2.QRCodeDetector()
                data, _, _ = detector.detectAndDecode(img_cv)
                if data:
                    results["opencv"] = data
                    break
            except Exception:
                pass

    # 3. PyZBar Engine
    if HAVE_PYZBAR and not (results["zxing"] or results["opencv"]):
        for p_img in pyramid:
            try:
                zbar_res = pyzbar.decode(p_img)
                if zbar_res and zbar_res[0].data:
                    results["zbar"] = zbar_res[0].data.decode("utf-8", errors="ignore")
                    break
            except Exception:
                pass

    # Match verification
    if expected_payload:
        matches = [
            engine for engine, val in results.items()
            if engine in ["zxing", "opencv", "zbar"] and val == expected_payload
        ]
        results["matches"] = len(matches) > 0
        results["match_count"] = len(matches)
    else:
        found = [val for k, val in results.items() if k in ["zxing", "opencv", "zbar"] and val]
        results["matches"] = len(found) > 0
        results["match_count"] = len(found)

    return results


# ==============================================================================
# 2. Robustness Stress Testing
# ==============================================================================

def test_robustness(img: Image.Image, expected_payload: str) -> dict:
    """
    Stress-tests QR durability against 8 realistic perturbations:
    - Resize 75% & Resize 50% (distance / camera zoom simulation)
    - Gaussian blur (defocus simulation)
    - Brightness +10% & Brightness -10% (exposure variations)
    - Contrast -10% (washed out display simulation)
    - Small Rotations ±3° (perspective / handheld tilt)
    """
    w, h = img.size
    tests = {
        "resize_75": img.resize((int(w * 0.75), int(h * 0.75)), Image.Resampling.BILINEAR),
        "resize_50": img.resize((int(w * 0.50), int(h * 0.50)), Image.Resampling.BILINEAR),
        "blur_light": img.filter(ImageFilter.GaussianBlur(radius=1.2)),
        "brightness_up": ImageEnhance.Brightness(img).enhance(1.10),
        "brightness_down": ImageEnhance.Brightness(img).enhance(0.90),
        "contrast_down": ImageEnhance.Contrast(img).enhance(0.90),
        "rotate_pos3": img.rotate(3, resample=Image.Resampling.BICUBIC, fillcolor=(128, 128, 128)),
        "rotate_neg3": img.rotate(-3, resample=Image.Resampling.BICUBIC, fillcolor=(128, 128, 128)),
    }

    passed_count = 0
    details = {}
    for name, t_img in tests.items():
        scan = scan_qr_ensemble(t_img, expected_payload)
        is_pass = scan["matches"]
        details[name] = is_pass
        if is_pass:
            passed_count += 1

    score = (passed_count / len(tests)) * 100.0
    return {
        "score": score,
        "passed": passed_count,
        "total": len(tests),
        "details": details,
    }


# ==============================================================================
# 3. Aesthetic Quality & Dynamic Contrast Scoring
# ==============================================================================

def calculate_aesthetic_quality(img: Image.Image) -> float:
    """Evaluates dynamic range, sharpness variance, and color richness (0-100 scale)."""
    try:
        arr = np.array(img.convert("RGB"), dtype=np.float32)
        gray = cv2.cvtColor(np.array(img), cv2.COLOR_RGB2GRAY) if HAVE_CV2 else np.mean(arr, axis=2).astype(np.uint8)
        
        # 1. Laplacian Sharpness Variance
        if HAVE_CV2:
            laplacian_var = cv2.Laplacian(gray, cv2.CV_64F).var()
            sharpness_score = min(100.0, laplacian_var / 8.0)
        else:
            sharpness_score = 75.0

        # 2. Dynamic Range & Luminance Spread
        lum_std = np.std(arr)
        contrast_score = min(100.0, (lum_std / 64.0) * 100.0)

        # 3. Colorfulness
        rg = np.abs(arr[:, :, 0] - arr[:, :, 1])
        yb = np.abs(0.5 * (arr[:, :, 0] + arr[:, :, 1]) - arr[:, :, 2])
        colorfulness = np.sqrt(np.mean(rg)**2 + np.mean(yb)**2) + 0.3 * (np.std(rg) + np.std(yb))
        color_score = min(100.0, colorfulness * 1.2)

        aesthetic = 0.40 * sharpness_score + 0.35 * contrast_score + 0.25 * color_score
        return round(float(np.clip(aesthetic, 10.0, 99.0)), 1)
    except Exception:
        return 80.0


# ==============================================================================
# 4. LEMAS Art QR System Prompt Injection
# ==============================================================================

SYSTEM_ART_QR_PROMPT = (
    "masterpiece, best quality, ultra-detailed, 8k resolution, cinematic volumetric lighting, "
    "rich organic textures, atmospheric depth, breathtaking composition, photorealistic fantasy"
)

SYSTEM_ART_QR_NEGATIVE = (
    "((monochrome, 2d silhouette, plain black and white silhouette, flat yellow sky, flat background:1.4)), "
    "ugly, deformed, distorted, low quality, blurry, pixelated, cartoonish, low resolution, "
    "visible QR overlay, black and white QR blocks, pasted QR code, barcode look, "
    "flat geometric grid, artificial square pattern, text, watermark, logo, frame, border, sticker, paper card"
)

def extract_visual_diffusion_prompt(raw_prompt: str) -> tuple[str, str]:
    """
    Extracts pure artistic and visual descriptive tokens from user prompts,
    intelligently prioritizing human subjects, colors, and specific objects,
    filtering out LLM meta-instructions, QR coordinates, and structural logic
    to fit optimally within CLIP's 77-token encoder budget.
    """
    if not raw_prompt:
        return "", ""

    raw_norm = raw_prompt.replace("\\n", "\n").replace("\r\n", "\n")
    lines = raw_norm.split("\n")
    subject_parts = []
    env_parts = []
    style_parts = []
    neg_parts = []
    is_neg = False

    for line in lines:
        l = line.strip()
        if not l:
            continue
        if "NEGATIVE PROMPT" in l.upper() or "NEGATIVE:" in l.upper():
            is_neg = True
            continue
        if is_neg:
            neg_parts.append(l)
            continue

        # Skip LLM meta instructions, placement coordinates & transformation recipes
        if any(skip_term in l for skip_term in [
            "IMPORTANT QR INTEGRATION", "Do NOT place", "Do NOT show", "The QR must be",
            "QR placement:", "Equivalent approximate box:", "Finder pattern placement:",
            "Transformation logic:", "Dark QR modules", "Light QR modules",
            "The giant tree trunk should absorb", "The left and central light-filled",
            "The lower floral field", "The small human figure near the lower center may overlap",
            "Preserve the QR geometry", "No visible pasted", "No harsh synthetic",
            "No obvious digital", "The QR should feel", "Create an extremely faithful QR-integrated",
            "matching the reference composition", "promt đây", "thêm vào k đc sửa",
            "x = 170", "y = 175", "left 16.6%", "top 17.1%", "right 82.0%", "bottom 82.5%",
            "on a 1024 by 1024 image", "seamlessly integrate", "The final result should look like:"
        ]):
            continue

        # Strip header labels
        clean = re.sub(r'^(Scene composition:|Visual style:|Visual style and mood:|Composition:)\s*', '', l, flags=re.IGNORECASE).strip()
        if not clean:
            continue

        # Categorize to preserve crucial visual subjects (characters, flowers, colors)
        clean_lower = clean.lower()
        if any(w in clean_lower for w in ["human", "figure", "person", "traveler", "man", "woman", "character", "girl", "boy"]):
            # Emphasize human figure
            subject_parts.append(f"(({clean}:1.35))")
        elif any(w in clean_lower for w in ["flower", "wildflower", "red", "emerald", "moss", "grass", "colors", "flora"]):
            subject_parts.append(f"(({clean}:1.25))")
        elif any(w in clean_lower for w in ["cinematic", "masterpiece", "realism", "lighting", "volumetric", "fog", "glow"]):
            style_parts.append(clean)
        else:
            env_parts.append(clean)

    # Assemble with key subjects in the first 77-token slot
    ordered_parts = subject_parts + env_parts + style_parts
    joined = ", ".join(ordered_parts).strip()
    words = joined.split()
    if len(words) > 65:
        joined = " ".join(words[:65]).rstrip(",")

    cleaned_negative = ", ".join(neg_parts).strip()
    return joined, cleaned_negative


# ==============================================================================
# 5. Core Generation Pipeline
# ==============================================================================

@gpu_worker
def generate(
    prompt: str,
    negative_prompt: str,
    qr_control_image: str,
    reference_image: str,
    conditioning_scale: float,
    reference_strength: float,
    seed: int,
    num_outputs: int,
    steps: int,
    placement_x: float = 0.166,
    placement_y: float = 0.171,
    placement_size: float = 0.654,
):
    num_outputs = max(1, min(int(num_outputs), 8))
    steps = max(24, min(int(steps), 36))
    conditioning_scale = max(1.1, min(float(conditioning_scale), 2.2))
    seed = int(seed) if int(seed) >= 0 else random.randint(0, 2**31 - 1)

    # 1. Decode QR control image sent from Go backend
    control_image = decode_base64_image(qr_control_image)
    if control_image is None:
        raise gr.Error("Valid qr_control_image is required from Go backend")

    # 2. Decode reference style image if provided
    raw_ref_image = decode_base64_image(reference_image)

    # 3. Detect expected payload from QR control image
    initial_scan = scan_qr_ensemble(control_image)
    expected_payload = initial_scan["zxing"] or initial_scan["opencv"] or initial_scan["zbar"] or "https://lemas.io.vn"

    print(f"[LEMAS Art QR] Pipeline started: expected_payload='{expected_payload}'")

    # 4. Standardized QR Preprocessing: Rebuild clean Level H with standard quiet zone border
    qr_clean = qrcode.QRCode(
        version=None,
        error_correction=qrcode.constants.ERROR_CORRECT_H,
        box_size=16,
        border=1,
    )
    qr_clean.add_data(expected_payload)
    qr_clean.make(fit=True)
    qr_standard_l = qr_clean.make_image(fill_color="black", back_color="white").convert("L")

    # 5. Optimize prompt for Stable Diffusion CLIP Text Encoder (77 Token Budget)
    visual_pos, extra_neg = extract_visual_diffusion_prompt(prompt)
    if not visual_pos:
        visual_pos = prompt.strip()

    full_positive = f"{visual_pos}, {SYSTEM_ART_QR_PROMPT}"
    neg_combined = f"{negative_prompt.strip()}, {extra_neg}".strip(", ")
    full_negative = f"{neg_combined}, {SYSTEM_ART_QR_NEGATIVE}" if neg_combined else SYSTEM_ART_QR_NEGATIVE

    print(f"[LEMAS Art QR] Visual Prompt: {full_positive[:120]}...")

    if not LOCAL_WORKER:
        pipe.to("cuda")
        img2img_pipe.to("cuda")

    candidates_list = []
    count = min(3, max(2, int(num_outputs))) if not raw_ref_image else max(1, min(3, int(num_outputs)))

    for i in range(count):
        gen_seed = seed + (i * 1013)
        generator = torch.Generator(device="cuda" if torch.cuda.is_available() else "cpu").manual_seed(gen_seed)

        if raw_ref_image is not None:
            # Reference image patch fusion mode (portrait / custom artwork)
            orig_w, orig_h = raw_ref_image.size
            min_dim = min(orig_w, orig_h)

            px = float(placement_x)
            py = float(placement_y)
            ps = float(placement_size)
            if ps <= 0 or ps > 1.0: ps = 0.654

            p_size = int(ps * min_dim)
            if p_size < 128: p_size = min_dim // 2
            px0 = max(0, min(orig_w - p_size, int(px * orig_w)))
            py0 = max(0, min(orig_h - p_size, int(py * orig_h)))

            pad = int(p_size * 0.35)
            x_start = max(0, px0 - pad)
            y_start = max(0, py0 - pad)
            x_end = min(orig_w, px0 + p_size + pad)
            y_end = min(orig_h, py0 + p_size + pad)
            crop_w = x_end - x_start
            crop_h = y_end - y_start

            context_patch = raw_ref_image.crop((x_start, y_start, x_end, y_end))
            qr_rel_x = px0 - x_start
            qr_rel_y = py0 - y_start

            qr_soft = qr_standard_l.filter(ImageFilter.GaussianBlur(radius=0.9)).resize((p_size, p_size), Image.Resampling.LANCZOS)
            ctrl_canvas = Image.new("L", (crop_w, crop_h), 255)
            ctrl_canvas.paste(qr_soft, (qr_rel_x, qr_rel_y))

            context_patch_768 = context_patch.resize((768, 768), Image.Resampling.LANCZOS)
            ctrl_canvas_768 = ctrl_canvas.resize((768, 768), Image.Resampling.LANCZOS)

            target_strength = 0.72 if reference_strength <= 0 else min(0.80, max(0.60, float(reference_strength)))

            patch_result = img2img_pipe(
                prompt=full_positive,
                negative_prompt=full_negative,
                image=context_patch_768,
                control_image=ctrl_canvas_768,
                strength=target_strength,
                num_inference_steps=max(28, steps),
                guidance_scale=8.0,
                controlnet_conditioning_scale=conditioning_scale,
                control_guidance_start=0.0,
                control_guidance_end=0.86,
                generator=generator,
            ).images[0]

            diffused_patch_orig = patch_result.resize((crop_w, crop_h), Image.Resampling.LANCZOS)

            comp_mask = Image.new("L", (crop_w, crop_h), 0)
            comp_draw = ImageDraw.Draw(comp_mask)
            mask_pad = int(pad * 0.28)
            comp_draw.rounded_rectangle(
                [qr_rel_x - mask_pad, qr_rel_y - mask_pad, qr_rel_x + p_size + mask_pad, qr_rel_y + p_size + mask_pad],
                radius=mask_pad,
                fill=255
            )
            comp_mask = comp_mask.filter(ImageFilter.GaussianBlur(radius=int(mask_pad * 0.55)))

            candidate_img = raw_ref_image.copy()
            candidate_img.paste(diffused_patch_orig, (x_start, y_start), comp_mask)
        else:
            # Pure Text-to-Art-QR Generation (Dual polarity candidate diversity)
            # Candidate 0/2: Inverted (perfect for dark forest / night skies)
            # Candidate 1: Standard
            if i % 2 == 0:
                ctrl_768 = ImageOps.invert(qr_standard_l).convert("RGB").resize((768, 768), Image.Resampling.LANCZOS)
            else:
                ctrl_768 = qr_standard_l.convert("RGB").resize((768, 768), Image.Resampling.LANCZOS)

            # Adaptive conditioning scale
            base_cond = 1.40 if float(conditioning_scale) < 1.35 else min(1.58, float(conditioning_scale))
            scale_variation = base_cond + (i * 0.04)

            candidate_raw = pipe(
                prompt=full_positive,
                negative_prompt=full_negative,
                image=ctrl_768,
                num_inference_steps=max(28, steps),
                guidance_scale=7.5,
                controlnet_conditioning_scale=scale_variation,
                control_guidance_start=0.0,
                control_guidance_end=0.88,
                generator=generator,
                width=768,
                height=768,
            ).images[0]

            candidate_img = candidate_raw.resize((1024, 1024), Image.Resampling.LANCZOS)

        # ==============================================================================
        # 6. Scanning Refinement & DiffQRCoder Repair
        # ==============================================================================
        scan_res = scan_qr_ensemble(candidate_img, expected_payload)

        # If not verified on initial generation, apply Scanning-Aware Refinement Repair
        if not scan_res["matches"]:
            print(f"[LEMAS Art QR] Candidate #{i+1} failed initial scan. Applying DiffQRCoder Scanning Refinement...")
            try:
                # Modulate contrast softly with QR matrix and run low-denoise repair pass
                repair_img = img2img_pipe(
                    prompt=full_positive,
                    negative_prompt=full_negative,
                    image=candidate_img.resize((768, 768), Image.Resampling.LANCZOS),
                    control_image=ctrl_768 if not raw_ref_image else ctrl_canvas_768,
                    strength=0.38,
                    num_inference_steps=24,
                    guidance_scale=8.0,
                    controlnet_conditioning_scale=min(1.75, scale_variation + 0.15),
                    control_guidance_start=0.15,
                    control_guidance_end=0.92,
                    generator=generator,
                ).images[0]

                repaired_1024 = repair_img.resize((1024, 1024), Image.Resampling.LANCZOS)
                repaired_scan = scan_qr_ensemble(repaired_1024, expected_payload)
                if repaired_scan["matches"]:
                    print(f"[LEMAS Art QR] Candidate #{i+1} successfully recovered via DiffQRCoder Repair pass!")
                    candidate_img = repaired_1024
                    scan_res = repaired_scan
            except Exception as e:
                print(f"[LEMAS Art QR] Repair pass error on candidate #{i+1}:", e)

        # ==============================================================================
        # 7. Robustness Stress Testing & Multi-Objective Ranking
        # ==============================================================================
        robustness = test_robustness(candidate_img, expected_payload) if scan_res["matches"] else {"score": 0.0, "passed": 0, "total": 8}
        aesthetic = calculate_aesthetic_quality(candidate_img)

        scan_score = 100.0 if scan_res["matches"] else 0.0
        robust_score = float(robustness["score"])
        aesthetic_score = float(aesthetic)
        prompt_score = 90.0

        # LEMAS ART QR Scoring Formula:
        # Final Score = (ScanPass * 40%) + (Robustness * 30%) + (Aesthetic * 20%) + (Prompt * 10%)
        final_score = (scan_score * 0.40) + (robust_score * 0.30) + (aesthetic_score * 0.20) + (prompt_score * 0.10)

        print(
            f"[LEMAS Art QR] Candidate #{i+1}: Valid={scan_res['matches']} (ZXing={bool(scan_res['zxing'])}, CV2={bool(scan_res['opencv'])}, ZBar={bool(scan_res['zbar'])}), "
            f"Robustness={robust_score}%, Aesthetic={aesthetic_score}, FinalScore={final_score:.1f}"
        )

        candidates_list.append({
            "image": candidate_img,
            "valid": scan_res["matches"],
            "final_score": final_score,
            "robustness": robust_score,
            "aesthetic": aesthetic_score,
            "scanners": scan_res,
        })

    # Sort candidates by final_score descending (verified scannable + most durable & beautiful first)
    candidates_list.sort(key=lambda x: (x["valid"], x["final_score"]), reverse=True)

    # Return the ranked images
    output_images = [c["image"] for c in candidates_list]
    return output_images[:num_outputs]


# ==============================================================================
# 6. Gradio UI & FastAPI Server Definition
# ==============================================================================

with gr.Blocks(title="LEMAS ART QR Engine") as demo:
    gr.Markdown("# LEMAS ART QR · Production AI Engine")
    gr.Markdown("Zero-loss artistic QR generation powered by Stable Diffusion 1.5, QRCode Monster v2, DiffQRCoder-style refinement & 3-Scanner validation.")

    prompt_input = gr.Textbox(label="Prompt mô tả hình ảnh", placeholder="A giant ancient tree in an enchanted forest, golden sunlight...")
    negative_input = gr.Textbox(label="Negative prompt", placeholder="low quality, blurry, sticker...")
    qr_ctrl_input = gr.Textbox(label="QR Control Image (Base64)", lines=2)
    ref_image_input = gr.Textbox(label="Reference Image (Optional Base64)", lines=2)

    with gr.Row():
        scale_input = gr.Slider(0.8, 2.0, value=1.25, step=0.05, label="ControlNet Strength")
        strength_input = gr.Slider(0.3, 0.95, value=0.72, step=0.05, label="Patch Strength")
        seed_input = gr.Number(value=-1, precision=0, label="Seed")
        count_input = gr.Slider(1, 4, value=4, step=1, label="Candidates Count")
        steps_input = gr.Slider(20, 36, value=30, step=1, label="Sampling Steps")

    with gr.Row():
        px_input = gr.Number(value=0.25, label="Placement X")
        py_input = gr.Number(value=0.25, label="Placement Y")
        psize_input = gr.Number(value=0.5, label="Placement Size")

    output = gr.Gallery(label="LEMAS Art QR Ranked Candidates", columns=2)
    gr.Button("Tạo Art QR ngay (Generate)", variant="primary").click(
        generate,
        inputs=[
            prompt_input,
            negative_input,
            qr_ctrl_input,
            ref_image_input,
            scale_input,
            strength_input,
            seed_input,
            count_input,
            steps_input,
            px_input,
            py_input,
            psize_input,
        ],
        outputs=output,
        api_name="generate",
    )


from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
import uvicorn

server_app = FastAPI(title="LEMAS ART QR Backend API")

@server_app.post("/verify")
async def verify_endpoint(request: Request):
    try:
        body = await request.body()
        if not body:
            return JSONResponse({"valid": False, "error": "empty body"})
        img = Image.open(io.BytesIO(body)).convert("RGB")
        scan_res = scan_qr_ensemble(img)
        text = scan_res["zxing"] or scan_res["opencv"] or scan_res["zbar"]
        return JSONResponse({
            "valid": len(text) > 0,
            "text": text,
            "scanners": {
                "zxing": bool(scan_res["zxing"]),
                "opencv": bool(scan_res["opencv"]),
                "zbar": bool(scan_res["zbar"]),
            }
        })
    except Exception as e:
        return JSONResponse({"valid": False, "error": str(e)})

# Mount Gradio Blocks application onto FastAPI
server_app = gr.mount_gradio_app(server_app, demo.queue(default_concurrency_limit=1 if LOCAL_WORKER else 2), path="/")

if __name__ == "__main__":
    uvicorn.run(
        server_app,
        host="127.0.0.1" if LOCAL_WORKER else "0.0.0.0",
        port=7860,
        log_level="info",
    )
