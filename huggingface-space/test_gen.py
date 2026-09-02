import base64
import json
import urllib.request
import os

# Test generation via local Gradio worker
qr_url = "https://api.qrserver.com/v1/create-qr-code/?size=512x512&data=https://lemas.io.vn"
qr_bytes = urllib.request.urlopen(qr_url).read()
qr_b64 = base64.b64encode(qr_bytes).decode("utf-8")

payload = {
    "data": [
        "Masterpiece portrait of an officer in military regalia with gold embroidery and red velvet jacket, photorealistic, intricate textures",
        "ugly, blurry, low quality, deformed, disfigured",
        qr_b64,
        "",
        1.35, # conditioning scale
        0.72, # strength
        42,   # seed
        1,    # count
        25,   # steps
        0.25, # px
        0.25, # py
        0.50, # psize
    ]
}

req = urllib.request.Request(
    "http://127.0.0.1:7860/gradio_api/call/generate",
    data=json.dumps(payload).encode("utf-8"),
    headers={"Content-Type": "application/json"}
)

resp = urllib.request.urlopen(req)
res_data = json.loads(resp.read().decode("utf-8"))
event_id = res_data.get("event_id")
print("Event ID:", event_id)

if event_id:
    res_req = urllib.request.Request(f"http://127.0.0.1:7860/gradio_api/call/generate/{event_id}")
    with urllib.request.urlopen(res_req) as response:
        for line in response:
            line_str = line.decode("utf-8").strip()
            if line_str.startswith("data:"):
                data_json = line_str[5:].strip()
                try:
                    result = json.loads(data_json)
                    print("SUCCESS:", json.dumps(result)[:200])
                    break
                except Exception:
                    pass
