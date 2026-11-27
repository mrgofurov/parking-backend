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
Boshqaruv turi `HIKVISION_IO` tanlanadi. So'ng kameraning IP manzili, login, paroli va relay chiqish raqami (odatda 1) kiritiladi. Backend `PUT /ISAPI/System/IO/outputs/{n}/trigger` buyrug'ini Digest auth bilan yuboradi: avval `high`, 1 soniyadan keyin `low`, ya'ni bitta impuls. Kamera ISAPI orqali kelgan `high`ni Alarm Output'dagi Delay sozlamasiga qaramay `low` kelguncha ushlab turadi, shuning uchun `low` backendning o'zi yuboradi. Kamerada qo'shimcha sozlash shart emas. Simni kameraning alarm chiqishidan (NO/COM) shlagbaum kontrollerining OPEN kirishiga ulang. Shlagbaum kontrolleri Hikvision'ning `barrierGate` buyrug'ini qo'llab-quvvatlasa, `HIKVISION_GATE` turini tanlang. Admin panelda **Tekshirish** tugmasi qurilma bilan aloqani va login to'g'riligini tekshiradi, **Ochish** tugmasi shlagbaumni hozir ochib ko'radi.

### Yo'lak monitori
Brauzerda `http://<admin-manzil>/display?camera=cam-exit` sahifasini to'liq ekran (kiosk) rejimida oching. Sahifa login so'ramaydi va WebSocket orqali real vaqtda yangilanadi.

### Hikvision LED ekran (DS-TVL224)
Kameraga `display_ip` (ekran IP manzili, standart `192.168.1.99`) va `display_rows` (`4` yoki `2`) kiritilsa, o'sha yo'lak holati LED ekranga ham chiqadi. Ekranda 64×64 piksel bor, har qatorga 8 ta lotin belgisi sig'adi, ranglar qizil, yashil va sariq. Chiqishda summa ko'rsatiladigan maket:

```
01A123BC   (sariq)  davlat raqami
2:35       (yashil) turgan vaqti
12 000     (qizil)  summa
SO'M       (qizil)
```

> Ekranning tarmoq protokoli hali ulanmagan. Hozircha backend ekranga chiqishi kerak bo'lgan matnni faqat logga yozadi (`[LED 192.168.1.99] [...]`). Protokol aniqlangach, `internal/pkg/ledscreen` paketiga haqiqiy drayver qo'shiladi.

### Telefon orqali to'lov (doimiy QR stiker + Click)
LED ekran QR chiqara olmaydi. Shuning uchun har bir chiqish yo'lagiga doimiy QR stiker yopishtiriladi, u `<PUBLIC_PAY_URL>/<kamera kodi>` sahifasini ochadi (masalan `https://parking.example.uz/pay/cam-exit`). Sahifa (frontend) ma'lumotni `GET /api/v1/pay/:camera` dan oladi:

| `state` | Ma'nosi | Sahifa nima ko'rsatadi |
|---|---|---|
| `PAYABLE` | Summa qotirilgan, Click havolasi tayyor | Raqam, vaqt, summa va `click_url` ga olib boruvchi **"Click orqali to'lash"** tugmasi |
| `CASHIER` | To'lov kerak, lekin onlayn to'lov mavjud emas | `message` (kassada to'lash) |
| `PAID` / `FREE` | Yo'l ochiq | `message` |
| boshqalar (`IDLE`, ...) | Yo'lakda to'lov kutilayotgan mashina yo'q | `message` |

`click_mode` = `mock` bo'lsa, tugma Click o'rniga `POST /api/v1/click/mock-pay/:bill_number` ni chaqiradi (sinov).

**Click oqimi:** havoladagi `transaction_param` — to'lov so'rovining `bill_number`i. Click to'lovni `POST /api/v1/click/prepare` va `POST /api/v1/click/complete` ga yuboradi (SHOP API, `sign_string` md5 bilan tekshiriladi). `Complete` kelishi bilan sessiya yopiladi, shlagbaum ochiladi va LED'da "TO'LANDI" chiqadi. Agar shu orada mashina kassada to'lagan bo'lsa, Click'ga `-9` qaytariladi va Click pulni qaytaradi.

**Click'ni ishga tushirish uchun:**
1. Click bilan merchant shartnomasi tuziladi. Ular `CLICK_SERVICE_ID`, `CLICK_MERCHANT_ID`, `CLICK_MERCHANT_USER_ID` va `CLICK_SECRET_KEY` ni beradi.
2. Click'ga Prepare va Complete URL'lari beriladi: `https://<domen>/api/v1/click/prepare` va `https://<domen>/api/v1/click/complete`. Backend internetdan HTTPS orqali ochiq bo'lishi shart.
3. `.env` da `CLICK_MODE=live` va `PUBLIC_PAY_URL` to'ldiriladi.

Munis o'chiq (`MUNIS_MODE=off`) bo'lsa ham Click ishlaydi: to'lov so'rovi QR'siz yaratiladi va faqat Click orqali to'lanadi.

### Munis sozlamalari (`.env`)
`MUNIS_MODE` qiymatlari:
- `live` — haqiqiy to'lovlar;
- `mock` — sinov rejimi: admin paneldagi "Sinov: QR to'lovini simulyatsiya qilish" tugmasi to'lovni tushgandek qiladi;
- `off` — QR ko'rsatilmaydi, faqat kassa.

`live` uchun kerakli sozlamalar: `MUNIS_QR_API_URL`, `MUNIS_QR_AUTH_TOKEN` (QR yaratish), `MUNIS_API_URL`, `MUNIS_API_USERNAME`, `MUNIS_API_PASSWORD` (to'lovni tekshirish). To'lovni tekshirish API'si bank tarmog'ida joylashgan, shuning uchun parking serveridan unga IPsec tunnel orqali kirish bo'lishi shart. To'liq ro'yxat `.env.example` faylida.

## Device-agent (backend bulutda, uskunalar parkovkada)

Backend parkovka tarmog'idan tashqarida (masalan chet eldagi VPS'da) ishlasa, parkovkadagi kichik kompyuterga `cmd/device-agent` o'rnatiladi. U backendga o'zi ulanadi, shlagbaum buyruqlarini lokal tarmoqda bajaradi va kamera hodisalarini navbat bilan bulutga uzatadi. Shlagbaumga `agent_id` berilsa, uning buyruqlari agent orqali ketadi. O'rnatish: [deploy/device-agent/README.md](deploy/device-agent/README.md).

## VPS'ga deploy (GitHub Actions)

`master`ga push qilinganda `.github/workflows/deploy.yml` testlarni o'tkazadi va kodni serverga ko'chirib, `docker-compose.prod.yml` (PostgreSQL va backend) bilan ishga tushiradi. Serverni bir marta tayyorlash, secret'lar va birinchi deploy tartibi: [deploy/SERVER.md](deploy/SERVER.md).
