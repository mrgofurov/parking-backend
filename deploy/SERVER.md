# VPS'ga deploy (GitHub Actions)

Uchala repozitoriyda `.github/workflows/deploy.yml` bor. `master`ga push qilinganda yoki Actions sahifasida **Deploy → Run workflow** bosilganda:

1. Testlar yoki build ishlaydi.
2. Kod `rsync` orqali serverdagi `$DEPLOY_PATH/<repo>` papkasiga ko'chiriladi.
3. Serverda `docker compose -f docker-compose.prod.yml up -d --build` bajariladi.
4. Xizmat javob berayotgani tekshiriladi. Javob bo'lmasa, workflow loglarni ko'rsatib xato bilan tugaydi.

```
/var/www/smart-parking/              ($DEPLOY_PATH)
├── parking-backend/          .env  → parking_db (PostgreSQL) + parking_backend   127.0.0.1:8085
├── parking-admin/           [.env] → parking_admin                              127.0.0.1:3000
└── parking-capture-service/  .env  → parking_capture_service                    127.0.0.1:8086
          └── hammasi "smart-parking" Docker tarmog'ida

Serverdagi nginx (HTTPS, Let's Encrypt):
  https://adminpark.duckdns.org    → 127.0.0.1:3000  admin panel
  https://apipark.duckdns.org      → 127.0.0.1:8085  backend API (device-agent, kamera, to'lov callback)
  https://capturepark.duckdns.org  → 127.0.0.1:8086  capture-service (parol bilan)
```

Konteynerlar faqat `127.0.0.1`da ochiladi, tashqaridan ularga faqat serverdagi nginx orqali kiriladi. Nginx sozlamasi alohida faylda turadi (`/etc/nginx/sites-available/smart-parking`). Serverdagi boshqa saytlar, masalan `xabarbek-admin`, o'zgarmaydi.

## Secret'lar (har bir repoda)

| Secret | Majburiy | Ma'nosi |
|---|---|---|
| `SSH_HOST` | ha | server IP manzili yoki domeni |
| `SSH_PORT` | yo'q (22) | SSH porti |
| `SSH_USER` | ha | deploy qiladigan foydalanuvchi |
| `SSH_KEY` | ha | shu foydalanuvchining **maxfiy** SSH kaliti (to'liq, `-----BEGIN ...` bilan) |
| `DEPLOY_PATH` | yo'q | standart qiymati `/var/www/smart-parking` |
| `SSH_KNOWN_HOSTS` | yo'q | serverning host kaliti (`ssh-keyscan -p PORT HOST` natijasi). Berilmasa, har safar `ssh-keyscan` bilan olinadi |

## Serverni bir marta tayyorlash

```bash
# 1. Docker, compose, rsync, curl
curl -fsSL https://get.docker.com | sudo sh
sudo apt-get install -y rsync curl

# 2. Deploy foydalanuvchisi Docker'ni ishlata olsin va papka uniki bo'lsin
sudo usermod -aG docker "$USER"          # keyin SSH'dan chiqib qayta kiring
sudo chown -R "$USER": /var/www/smart-parking

# 3. SSH_KEY ning ochiq qismi shu foydalanuvchida bo'lsin
cat deploy_key.pub >> ~/.ssh/authorized_keys

```

3000, 8085 va 8086 portlari faqat `127.0.0.1`da ochiladi, ularni firewall'da ochish kerak emas. Bu portlarni serverda boshqa dastur band qilmaganini tekshiring: `sudo ss -ltnp | grep -E ':(3000|8085|8086) '`.

## Birinchi deploy tartibi

1. **parking-backend:** workflow'ni ishga tushiring. Birinchi marta u `.env` topmaydi, namunadan (`deploy/production.env.example`) yaratadi va xato bilan to'xtaydi. Serverda faylni to'ldiring:
   ```bash
   cd /var/www/smart-parking/parking-backend
   nano .env      # POSTGRES_PASSWORD va JWT_SECRET: openssl rand -hex 24 / openssl rand -hex 32
   ```
   Keyin workflow'ni qayta ishga tushiring. PostgreSQL va backend ko'tariladi, jadvallar o'zi yaratiladi.
2. **parking-admin:** workflow'ni ishga tushiring.
3. **parking-capture-service:** birinchi marta `.env` namunadan yaratiladi. Kamera yoki video sozlamalarini to'ldirib, qayta ishga tushiring. Birinchi build `torch` o'rnatgani uchun 10–20 daqiqa davom etadi.

4. **HTTPS (bir marta, 3-qadamdan keyin yoki oldin):** quyidagi "Domenlar va HTTPS" bo'limiga qarang. Shundan keyin panel `https://adminpark.duckdns.org` manzilida ochiladi.

**Birinchi kirishdan keyin darhol** `admin` / `admin123` va `operator` / `operator123` parollarini almashtiring. Backend bu foydalanuvchilarni bo'sh bazada avtomatik yaratadi.

## Capture-service qayerda ishlashi kerak

VPS'dagi capture-service parkovkadagi kameralarni ko'rmaydi, chunki ular lokal tarmoqda. Haqiqiy parkovka uchun capture-service'ni parkovkadagi mini-PC'da, device-agent yonida ishga tushiring (`BACKEND_URL=http://<agent-ip>:8090`), qarang: [device-agent/README.md](device-agent/README.md). VPS'dagi nusxa video fayl bilan sinash yoki internet orqali ochiq RTSP kamera uchun yaraydi.

## Domenlar va HTTPS

Domenlar serverga (`91.207.29.64`) yo'naltirilgan. Serverda bir marta ishga tushiring (`deploy` papkasi backend deploy'i bilan serverga allaqachon ko'chirilgan):

```bash
cd /var/www/smart-parking/parking-backend/deploy/nginx
sudo ./setup-https.sh sizning@email.uz
```

Skript quyidagilarni bajaradi:
1. `certbot` o'rnatadi.
2. Uchala domen uchun nginx saytini yozadi.
3. Capture-service uchun login va parol so'raydi. Uning API'si kamera sozlamalarini o'zgartira oladi va o'z logini yo'q, shu sabab parol qo'yiladi. Admin panel capture-service'ga Docker ichida ulanadi, shuning uchun bu parol unga ta'sir qilmaydi.
4. Let's Encrypt sertifikatini oladi va HTTP so'rovlarni HTTPS'ga yo'naltiradi. Sertifikat avtomatik yangilanadi.

**Muhim:** hozir serverda 443-port tashqaridan yopiq. Server provayderining panelida firewall yoki security group bo'lsa, u yerda **80 va 443** portlarini oching. ufw yoqilgan bo'lsa, skript uni o'zi ochadi.

Keyin quyidagilar bo'ladi:
- Device-agent'larda `AGENT_SERVER_URL=https://apipark.duckdns.org`.
- Parkovkadagi capture-service `BACKEND_URL` sifatida agentni ishlatadi (`http://<agent-ip>:8090`), agent esa API domeniga ulanadi.
- Admin panel shu API manzilini agent tokeni oynasida ko'rsatadi. Boshqa domen kerak bo'lsa, `parking-admin/.env`ga `PUBLIC_API_URL=...` yozing va admin'ni qayta deploy qiling.

## Kundalik buyruqlar

```bash
cd /var/www/smart-parking/parking-backend
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs -f backend
docker compose -f docker-compose.prod.yml restart backend

# Bazaning zaxira nusxasi (har kuni cron bilan boshqa joyga ko'chiring)
docker exec parking_db pg_dump -U parking parking_db | gzip > ~/parking_db_$(date +%F).sql.gz
```

Ma'lumotlar Docker volume'larida saqlanadi: `smart-parking-backend_pgdata` (baza) va `smart-parking-backend_uploads` (rasmlar). Deploy ularga tegmaydi.
