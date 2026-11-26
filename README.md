# Smart Parking System — Backend Service (`parking-backend`)

Tijorat darajasidagi Smart Parking tizimi uchun yuqori unumdorlikka ega Go (Fiber v2) backend xizmati.

## Asosiy imkoniyatlar
- **Toza Arxitektura (Clean Architecture)**: Domain, Repository, Service, Delivery qatlamlari.
- **ANPR Webhook Integratsiyasi**: Hikvision, Dahua va Universal kameralardan signallarni qabul qilish.
- **Anti-Duplicate Filter**: Kameralar bir xil avtomobilni qisqa vaqt oralig'ida qayta o'qiganida takroriy sessiya ochilishini oldini oladi.
- **Shlagbaum Relay Kontroleri**: Mashina kelganda va to'lov muvaffaqiyatli bo'lganda avtomatik ochilish, sensor/vaqt bo'yicha avtomatik yopilish.
- **Tariflar va Mijoz Chegirmalari**: UZS (so'm) valyutasida soatlik, bepul daqiqalar va VIP/Xodim chegirmalari (100% bepul, 1 soat bepul, 3 soat bepul).
- **To'lovlar va Termal Chek**: Naqd, karta, QR to'lovlar, 80mm printerga mos to'liq kvitansiya ma'lumotlari.
- **Real-Time WebSocket**: Jonli monitor, shlagbaum animatsiyasi va avtomobillar kirib-chiqishini sahifani yangilamasdan uzatish.
- **Ko'p Ma'lumotlar Bazasi**: PostgreSQL va SQLite qo'llab-quvvatlash.

---

## Boshlang'ich Ma'lumotlar (Seed Data)
Tizim birinchi marta ishga tushganida quyidagi ma'lumotlar avtomatik yaratiladi:
* **Admin Login**: `admin` / Parol: `admin123`
* **Operator Login**: `operator` / Parol: `operator123`
* **Standart Tarif**: 15 daqiqa bepul, soatiga 5 000 so'm, kunlik maksimal chegara 60 000 so'm
* **Demo VIP mijozlar**:
  * `01A777AA`: 100% Bepul (Direktor)
  * `01B123BB`: 1 soat bepul (Mijoz)
  * `01C999CC`: 3 soat bepul (Xodim)

---

## Ishga tushirish

### Mahalliy (Local):
```bash
cd parking-backend
go run cmd/server/main.go
```
Server `http://localhost:8080` portida ishga tushadi.

### Docker orqali:
```bash
docker build -t smart-parking-backend .
docker run -p 8080:8080 smart-parking-backend
```

---

## Asosiy API Endpointlar

### 1. Autentifikatsiya
* `POST /api/v1/auth/login` — Tizimga kirish (JWT token olish)
* `GET /api/v1/auth/me` — Joriy foydalanuvchi ma'lumotlari

### 2. ANPR Kamera & Simulyator
* `POST /api/v1/anpr/event` — Kamera webhook (JSON)
  ```json
  {
    "plate_number": "01A777AA",
    "direction": "ENTRY",
    "confidence": 98.2
  }
  ```
* `POST /api/v1/anpr/simulate` — Admin panelidan test simulyatsiya qilish

### 3. Shlagbaum Boshqaruvi
* `GET /api/v1/barriers` — Shlagbaumlar ro'yxati va holati (OPEN/CLOSED)
* `POST /api/v1/barriers/:id/open` — Shlagbaumni ochish
* `POST /api/v1/barriers/:id/close` — Shlagbaumni yopish
* `POST /api/v1/barriers/:id/car-passed` — Datchik signali (avtomobil o'tdi -> yopish)

### 4. Sessiyalar & To'lovlar
* `GET /api/v1/sessions` — Sessiyalar ro'yxati (filtrlash: plate, status, date)
* `POST /api/v1/sessions/:id/close` — Sessiyani qo'lda yopish
* `POST /api/v1/payments` — To'lov qabul qilish va chiqish shlagbaumi ochilishi
* `GET /api/v1/payments/:id/receipt` — Termal chek kvitansiyasi

### 5. WebSocket (Real-time)
* `ws://localhost:8080/ws` — Jonli hodisalar oqimi (`VEHICLE_ENTRY`, `VEHICLE_EXIT`, `BARRIER_STATUS`, `PAYMENT_SUCCESS`, `OCCUPANCY_UPDATE`)

---

## Kirish/chiqish oqimi (Hikvision + Munis UzQR)

1. **Kirish:** kamera raqamni o'qiydi → sessiya ochiladi → yo'lak shlagbaumi ochiladi.
2. **Chiqish:** raqam o'qiladi → summa hisoblanadi.
   - 0 so'm (bepul daqiqalar / VIP) bo'lsa, shlagbaum darhol ochiladi.
   - Aks holda Munis dinamik QR yaratiladi va yo'lak monitorida summa bilan birga ko'rsatiladi.
   - Backend har `PAYMENT_POLL_SECONDS` soniyada Munis'dan to'lovni tekshiradi. To'lov tushishi bilan chiqish shlagbaumi ochiladi.
   - Kassada naqd yoki karta orqali to'lov qabul qilinsa ham shlagbaum ochiladi, QR esa bekor qilinadi.

### Kamera sozlash (Admin → Qurilmalar)
| Kamera turi | Manba | Raqamni kim o'qiydi |
|---|---|---|
| Hikvision ANPR/LPR | `HIKVISION_ANPR` | Kamera o'zi. Hodisani `POST /api/v1/anpr/hikvision/<kod>` ga yuboradi |
| Oddiy IP kamera | `CAPTURE_SERVICE` | parking-capture-service (RTSP → YOLO + OCR). Hodisani `POST /api/v1/anpr/event` ga yuboradi |

**Hikvision kamera veb-interfeysida:** Configuration → Network → Advanced Settings → HTTP Listening (yoki Alarm Server):
- Destination IP: backend serverining IP manzili
- Port: `8085`
- URL: `/api/v1/anpr/hikvision/cam-entry` (har bir kamera uchun o'z kodi). Agar `ANPR_WEBHOOK_TOKEN` berilgan bo'lsa, oxiriga `?token=...` qo'shiladi.
- Event → ANPR (Vehicle Detection) → Linkage Method: **Notify Surveillance Center** yoqiladi.

Kamera URL'dagi kod bo'yicha aniqlanadi. Kod bo'lmasa, hodisa ichidagi IP manzil bo'yicha aniqlanadi.

### Shlagbaum (kamera relay chiqishi)
Boshqaruv turi `HIKVISION_IO` tanlanadi. So'ng kameraning IP manzili, login, paroli va relay chiqish raqami (odatda 1) kiritiladi. Backend `PUT /ISAPI/System/IO/outputs/{n}/trigger` buyrug'ini Digest auth bilan yuboradi. Kamerada Alarm Output'ni **pulse** rejimiga (1–3 soniya) sozlash kerak. Shlagbaum kontrolleri Hikvision'ning `barrierGate` buyrug'ini qo'llab-quvvatlasa, `HIKVISION_GATE` turini tanlang. Admin panelda **Tekshirish** tugmasi qurilma bilan aloqani va login to'g'riligini tekshiradi, **Ochish** tugmasi shlagbaumni hozir ochib ko'radi.

### Yo'lak monitori
Brauzerda `http://<admin-manzil>/display?camera=cam-exit` sahifasini to'liq ekran (kiosk) rejimida oching. Sahifa login so'ramaydi va WebSocket orqali real vaqtda yangilanadi.

### Munis sozlamalari (`.env`)
`MUNIS_MODE` qiymatlari:
- `live` — haqiqiy to'lovlar;
- `mock` — sinov rejimi: admin paneldagi "Sinov: QR to'lovini simulyatsiya qilish" tugmasi to'lovni tushgandek qiladi;
- `off` — QR ko'rsatilmaydi, faqat kassa.

`live` uchun kerakli sozlamalar: `MUNIS_QR_API_URL`, `MUNIS_QR_AUTH_TOKEN` (QR yaratish), `MUNIS_API_URL`, `MUNIS_API_USERNAME`, `MUNIS_API_PASSWORD` (to'lovni tekshirish). To'lovni tekshirish API'si bank tarmog'ida joylashgan, shuning uchun parking serveridan unga IPsec tunnel orqali kirish bo'lishi shart. To'liq ro'yxat `.env.example` faylida.
