# LIFF Test Kit — ดึง `line_user_id` แบบ verified

ชุดทดสอบสำหรับลอง flow "เปิดหน้าเว็บผ่าน LINE LIFF → ได้ `line_user_id` ที่ backend verify แล้ว"
ตามที่คุยกันไว้ใน ADR 0002 (LINE Identity Linking) — ยังไม่ใช่ของจริง แค่พิสูจน์ว่า flow ทำงานได้ก่อน

ไฟล์ในนี้:
- `index.html` — หน้าเว็บที่มี LIFF SDK, กด login แล้วยิง ID token ไปให้ backend verify
- backend endpoint ที่รองรับ: `POST /public/liff/verify` (ดู `internal/handlers/auth_handler.go` ฟังก์ชัน `VerifyLiffToken`)
- route ที่ serve หน้านี้: `GET /liff-test` (ดู `cmd/main.go`)

## Setup ทีละขั้น

### 1. สร้าง LIFF app ใน LINE Developers Console
1. เข้า https://developers.line.biz/console/ → เลือก Provider/Channel ที่มีอยู่แล้ว (หรือสร้างใหม่)
2. แท็บ **LIFF** → Add → ตั้งชื่อ, **Size** เลือก Full, **Scope** ติ๊ก `openid` และ `profile` (จำเป็น ถ้าไม่ติ๊ก `openid` จะไม่ได้ ID token)
3. **Endpoint URL** ใส่ URL ของ ngrok (ดูขั้นตอนที่ 3) ต่อท้ายด้วย `/liff-test` เช่น `https://xxxx.ngrok-free.app/liff-test`
4. บันทึกแล้วจด **LIFF ID** ที่ได้ (รูปแบบ `1234567890-AbCdEfGh`)
5. จดด้วยว่า Channel นี้ **Channel ID** คืออะไร (อยู่แท็บ Basic settings) — ต้องใส่ใน `.env` ของ backend

### 2. ตั้งค่า backend
ใน `mobile-backend/.env` ใส่:
```
LINE_CHANNEL_ID=<channel id จากขั้นตอน 1.5>
```
แล้ว restart server (`go run cmd/main.go`)

### 3. เปิด ngrok tunnel ชี้ที่ backend (port 8080)
```bash
ngrok http 8080
```
จะได้ URL แบบ `https://xxxx.ngrok-free.app` — เอา URL นี้ไปตั้งเป็น Endpoint URL ในขั้นตอน 1.3 (ต้องอัปเดตทุกครั้งที่ restart ngrok ถ้าใช้ free plan เพราะ URL เปลี่ยน)

### 4. ใส่ LIFF ID ในไฟล์ทดสอบ
เปิด `index.html` แก้บรรทัด:
```js
const LIFF_ID = "YOUR_LIFF_ID_HERE";
```
เป็น LIFF ID จากขั้นตอน 1.4

### 5. ทดสอบบนมือถือจริง (แนะนำ — LIFF ทำงานดีที่สุดในแอป LINE จริง)
ส่งลิงก์ `https://liff.line.me/<LIFF_ID>` ให้ตัวเองในแชท LINE (เช่นแชท "Keep") แล้วกดเปิดจากในแอป LINE
- ครั้งแรกจะมี consent screen ให้กด "อนุญาต"
- หน้าเว็บจะขึ้นสถานะทีละขั้น จนได้ `line_user_id` ที่ backend verify แล้วมาโชว์

ทดสอบผ่าน desktop browser ธรรมดาก็ได้เหมือนกัน (LIFF จะเปิดหน้า login ของ LINE ในแท็บใหม่ให้ login ก่อน) แต่พฤติกรรมจะไม่เหมือนตอนอยู่ใน LINE จริง 100%

## หมายเหตุ
- `/public/liff/verify` เป็น public route (ไม่ต้องมี JWT cookie ของระบบเรา) เพราะตอนนี้ยังเป็นแค่ "verify ตัวตนกับ LINE" เฉยๆ ยังไม่ได้ผูกกับบัญชีในระบบ — ขั้นตอนผูกบัญชีจริงเป็นงานถัดไป (ต้อง login เข้าบัญชีเดิมในหน้าเดียวกันด้วย ตามที่คุยกันไว้)
- ถ้า `liff.getIDToken()` คืน `null` ให้เช็คว่า LIFF app เปิด scope `openid` ไว้หรือยัง (ขั้นตอน 1.2)
- ngrok free tier มีหน้า interstitial warning ก่อนเข้าเว็บจริง — ถ้าเป็นปัญหาตอนเปิดใน LINE webview ให้ลองส่ง header `ngrok-skip-browser-warning: true` หรือพิจารณา deploy จริงแทน
