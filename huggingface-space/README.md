---
title: Lemas Art QR
emoji: 🎨
colorFrom: green
colorTo: indigo
sdk: gradio
sdk_version: 5.49.1
app_file: app.py
pinned: false
license: apache-2.0
models:
  - stable-diffusion-v1-5/stable-diffusion-v1-5
  - monster-labs/control_v1p_sd15_qrcode_monster
---

# Lemas Art QR ZeroGPU Worker

Private generation worker for Lemas. The public Gradio UI is intentionally minimal; production requests are sent through the named `generate` API endpoint.

The Go API decodes the uploaded QR locally and sends only its exact payload. This worker regenerates an error-correction-H QR on a module-aligned grid, uses QR Monster v2 for conditioning, and applies progressively stronger module correction to the four candidates.

Select **ZeroGPU** in the Space hardware settings after creating the Space.
