import json
import base64
import os

with open("server/artqr_jobs.json", "r", encoding="utf-8") as f:
    d = json.load(f)

for job_id, job in d.items():
    ref_b64 = job.get("reference_image_jpeg")
    if ref_b64:
        raw = base64.b64decode(ref_b64)
        print(f"Job {job_id} has reference image of size {len(raw)} bytes")
        with open("server/original_user_upload.jpg", "wb") as out:
            out.write(raw)
        break
