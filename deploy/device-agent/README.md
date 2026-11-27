# Device-agent: bulutdagi backend va parkovkadagi uskunalar

Backend chet eldagi VPS'da, kameralar va shlagbaumlar esa parkovkaning lokal tarmog'ida bo'lsa, parkovkadagi kichik kompyuterga **device-agent** o'rnatiladi.

```
 Parkovka (lokal tarmoq)                         VPS (bulut)
 ┌────────────────────────────────┐              ┌──────────────────────┐
 │ Kamera ──HTTP──► device-agent ─┼── wss:// ───►│ backend /agent/ws    │
 │ capture-service ─► :8090       │  (chiquvchi) │ backend /api/v1/anpr │
 │ Shlagbaum ◄─ISAPI/rele─ agent  │              └──────────────────────┘
 └────────────────────────────────┘
```

- Agent backendga **o'zi ulanadi** va bu aloqani doim ochiq ushlab turadi. Parkovkada port ochish, statik IP yoki router sozlash kerak emas, internet bo'lsa yetarli.
- Backend shlagbaumni ochish, yopish va tekshirish buyruqlarini shu aloqa orqali yuboradi. Agent ularni lokal tarmoqda bajaradi: Hikvision rele chiqishi, Hikvision barrier gate yoki HTTP rele. Kod backenddagi bilan bir xil (`internal/pkg/barrierctl`).
- Kamera va capture-service hodisalari agentning `:8090` portiga yuboriladi, agent ularni bulutga uzatadi. Internet uzilsa, hodisalar diskdagi navbatda kutadi va aloqa tiklanganda tartib bilan yetkaziladi.
- Har bir agentning o'z tokeni bor, bazada faqat tokenning xeshi saqlanadi. Token almashtirilsa, eski ulanish darhol uziladi.

## 1. Agent yaratish (admin)

Admin panelda **Kamera & Shlagbaum → Device-agentlar → Agent qo'shish** tugmasini bosing. Ochilgan oynada tayyor `agent.env` matni chiqadi, uni nusxa olib qo'ying. Token **faqat bir marta ko'rsatiladi**, yo'qolsa "Yangi token" tugmasi bilan yangisini yaratasiz. Shu bo'limda agent onlayn yoki oflaynligi, kompyuter nomi, versiyasi va unga bog'langan shlagbaumlar ko'rinadi.

API orqali ham yaratish mumkin:

```bash
curl -X POST https://apipark.duckdns.org/api/v1/agents \
  -H "Authorization: Bearer <admin JWT>" -H "Content-Type: application/json" \
  -d '{"name": "Markaziy filial — mini-PC"}'
```

Javobdagi `token` ham faqat bir marta ko'rsatiladi. Boshqa amallar:

| Amal | So'rov |
|---|---|
| Ro'yxat (onlayn yoki oflayn) | `GET /api/v1/agents` |
| Tokenni almashtirish | `POST /api/v1/agents/:id/rotate-token` |
| O'chirish | `DELETE /api/v1/agents/:id` (shlagbaumlar agentdan ajratiladi) |

## 2. Parkovkadagi mini-PC'ga o'rnatish

Binarni istalgan kompyuterda yig'ing (backend repozitoriysining ildizida):

```bash
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=0.1.0" -o parking-agent ./cmd/device-agent
```

`parking-agent` binarini va `deploy/device-agent/` papkasini mini-PC'ga ko'chiring, keyin:

```bash
sudo ./install.sh ./parking-agent
sudo nano /etc/parking-agent/agent.env      # AGENT_SERVER_URL va AGENT_TOKEN
sudo systemctl restart parking-agent
```

Tekshirish:

```bash
systemctl status parking-agent
journalctl -u parking-agent -f
curl http://localhost:8090/health            # "connected": true bo'lishi kerak
```

Mini-PC BIOS'ida "Restore on AC power loss = Power On" sozlamasini yoqing va kompyuterni UPS'ga ulang.

## 3. Shlagbaumni agentga bog'lash

Admin panelda shlagbaumni tahrirlang va **"Buyruqni kim yuboradi?"** maydonida parkovkadagi agentni tanlang. Shlagbaumning boshqa sozlamalari (turi, IP, login, rele raqami) o'zgarmaydi, ular parkovka tarmog'iga tegishli manzillar bo'ladi.

API orqali:

```bash
curl -X PUT https://apipark.duckdns.org/api/v1/barriers/<barrier id> \
  -H "Authorization: Bearer <admin JWT>" -H "Content-Type: application/json" \
  -d '{"agent_id": "<agent id>"}'
```

`"agent_id": ""` yuborilsa, shlagbaum agentdan ajratiladi va buyruqlarni yana backendning o'zi yuboradi. Admin paneldagi **Tekshirish** tugmasi javobida `via_agent: true` ko'rinadi. Agent oflayn bo'lsa, `AGENT_OFFLINE` holati qaytadi.

## 4. Kamera va capture-service

- **Hikvision ANPR kamera:** Network → Advanced → HTTP Listening bo'limida `http://<agent-ip>:8090/api/v1/anpr/hikvision/<kamera kodi>` manzilini kiriting.
- **Capture-service:** `.env` faylida `BACKEND_URL=http://<agent-ip>:8090` qiling. Uning barcha so'rovlari (`/api/v1/anpr/event`, `/heartbeat`) agent orqali o'tadi.
- Backendda `ANPR_WEBHOOK_TOKEN` o'rnatilgan bo'lsa, xuddi shu qiymatni agentning `AGENT_ANPR_TOKEN` sozlamasiga yozing.

## 5. VPS tomoni

Agent `https://apipark.duckdns.org` manziliga ulanadi. Serverdagi nginx bu domenni WebSocket'ni qo'llab-quvvatlagan holda backendga (`127.0.0.1:8085`) uzatadi, sozlamasi `deploy/nginx/smart-parking.conf` faylida. O'rnatish tartibi: [SERVER.md](../SERVER.md#domenlar-va-https).

## Internet uzilganda

- **Shlagbaum buyruqlari:** bajarilmaydi. Backend xato qaytaradi, shlagbaum holati o'zgarmaydi. Pult bilan boshqarish ishlashda davom etadi, chunki pult qabul qiluvchisi shlagbaumga to'g'ridan-to'g'ri ulangan.
- **Kamera hodisalari:** navbatga yoziladi va aloqa tiklangach tartib bilan yuboriladi. `AGENT_QUEUE_MAX_AGE` (standart 30 daqiqa) dan eski hodisalar tashlab yuboriladi. Sababi: kechikib kelgan chiqish hodisasi allaqachon ketgan mashina uchun to'lov ekranini ko'rsatadi.
- Agar javob kamera tomonga yetib kelmay qolsa, o'sha hodisa qayta yuborilishi mumkin, ya'ni backend uni ikki marta olishi mumkin.
