# LIFF Test Kit — ดึง `line_user_id` แบบ verified

ชุดทดสอบสำหรับลอง flow "เปิดหน้าเว็บผ่าน LINE LIFF → ได้ `line_user_id` ที่ backend verify แล้ว"
ตามที่คุยกันไว้ใน ADR 0002 (LINE Identity Linking) — ยังไม่ใช่ของจริง แค่พิสูจน์ว่า flow ทำงานได้ก่อน

ไฟล์ในนี้ (2 หน้า ใช้ LIFF ID เดียวกัน):
- `index.html` — verify เฉยๆ ไม่ login บัญชีเดิม ยิง ID token ไปให้ backend verify แล้วโชว์ `line_user_id`
  - serve ที่ `GET /liff-test` → เรียก `POST /public/liff/verify` (`VerifyLiffToken`)
- `link.html` — จำลอง flow "existing farmer": login บัญชีเดิม (username/password) + verify ตัวตน LINE พร้อมกัน
  - serve ที่ `GET /liff-link` → เรียก `POST /public/liff/link` (`LinkLineAccount`)
  - ตอนนี้ verify ผ่านแล้วยังไม่ insert ลง `auth.line_identity` เพราะตารางนี้ยังไม่ถูกสร้าง (รอ migration ตาม ADR 0005) — ดู comment `TODO` ใน `LinkLineAccount`

ทั้งสอง handler ใช้ helper กลาง `verifyLineIDToken()` ในการยิงไปเช็คกับ LINE (`internal/handlers/auth_handler.go`)

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
เปิด **ทั้ง** `index.html` และ `link.html` แก้บรรทัด (ต้องแก้ทั้งสองไฟล์ ใช้ค่าเดียวกัน):
```js
const LIFF_ID = "YOUR_LIFF_ID_HERE";
```
เป็น LIFF ID จากขั้นตอน 1.4

### 5. ทดสอบบนมือถือจริง (แนะนำ — LIFF ทำงานดีที่สุดในแอป LINE จริง)
ส่งลิงก์ `https://liff.line.me/<LIFF_ID>` ให้ตัวเองในแชท LINE (เช่นแชท "Keep") แล้วกดเปิดจากในแอป LINE — จะพาไปหน้าที่ตั้งเป็น Endpoint URL ไว้ (ตามขั้นตอน 1.3)

อยากทดสอบอีกหน้าที่ไม่ได้ตั้งเป็น Endpoint URL ไว้ (เช่นตั้ง Endpoint URL เป็น `/liff-test` ไว้ แต่อยากลอง `/liff-link` บ้าง) แก้ Endpoint URL ในคอนโซลชั่วคราวให้ชี้ไปอีกหน้า แล้วกดลิงก์ `https://liff.line.me/<LIFF_ID>` ซ้ำ (LIFF ID เดิม พาไปหน้าไหนก็ได้ตาม Endpoint URL ที่ตั้งไว้ ณ ตอนนั้น)
- ครั้งแรกจะมี consent screen ให้กด "อนุญาต"
- `link.html` จะขอ username/password ของบัญชีที่มีอยู่แล้วในระบบ (ตาราง `auth.user_account`) ต้องมี test user จริงในฐานข้อมูลก่อน

## หมายเหตุ
- `/public/liff/verify` กับ `/public/liff/link` เป็น public route ทั้งคู่ (ไม่ต้องมี JWT cookie ของระบบเรา) — `/liff/link` ตรวจ username/password เองข้างในแทน ไม่ได้พึ่ง cookie
- `link.html` verify ผ่านแล้วแต่ **ยังไม่ persist** การผูกบัญชีลง DB เพราะตาราง `auth.line_identity` ยังไม่ถูกสร้าง — ดู `TODO` ใน `LinkLineAccount`
- ถ้า `liff.getIDToken()` คืน `null` ให้เช็คว่า LIFF app เปิด scope `openid` ไว้หรือยัง (ขั้นตอน 1.2)
- ngrok free tier มีหน้า interstitial warning ก่อนเข้าเว็บจริง — ถ้าเป็นปัญหาตอนเปิดใน LINE webview ให้ลองส่ง header `ngrok-skip-browser-warning: true` หรือพิจารณา deploy จริงแทน
