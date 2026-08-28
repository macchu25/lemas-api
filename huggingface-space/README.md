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

Select **ZeroGPU** in the Space hardware settings after creating the Space.
