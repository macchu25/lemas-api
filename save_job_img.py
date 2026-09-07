import json
import base64

with open('server/artqr_jobs.json', 'r', encoding='utf-8') as f:
    d = json.load(f)

j = d.get('artqr_3afa3906-3aec-4041-a011-7bd132e00fd4')
imgs = j.get('images', [])
if imgs:
    url = imgs[0]['url']
    b64_data = url.split(',', 1)[1]
    raw = base64.b64decode(b64_data)
    with open('server/job_output_view.png', 'wb') as out:
        out.write(raw)
    print("Saved server/job_output_view.png successfully!")
