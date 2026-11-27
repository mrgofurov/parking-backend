SMART PARKING DEVICE-AGENT (WINDOWS)
====================================

Parkovkadagi Windows kompyuterga o'rnatiladi. Agent https://apipark.duckdns.org
ga o'zi ulanadi, shlagbaum buyruqlarini lokal tarmoqda bajaradi va kamera
hodisalarini bulutga uzatadi. Parkovkada port ochish yoki statik IP kerak emas.

Talablar: Windows 10/11 yoki Windows Server 2016+, 64-bit, internet.


1. TOKEN OLISH
--------------
Admin panel: https://adminpark.duckdns.org
  Kamera & Shlagbaum -> Device-agentlar -> "Agent qo'shish"
Chiqqan oynadagi AGENT_TOKEN ni nusxa oling (faqat bir marta ko'rsatiladi).


2. SOZLASH
----------
Shu papkadagi agent.env.example faylini agent.env nomi bilan nusxalang va
Notepad'da oching:
  AGENT_TOKEN=      <- admin paneldagi token
  AGENT_ANPR_TOKEN= <- serverdagi backend .env dagi ANPR_WEBHOOK_TOKEN

(agent.env ni yaratmasangiz, o'rnatuvchi uni o'zi yaratib Notepad'da ochadi.)


3. O'RNATISH
------------
install.cmd -> o'ng tugma -> "Run as administrator".

O'rnatuvchi:
  - agentni C:\Program Files\ParkingAgent ga ko'chiradi;
  - Windows Firewall'da 8090-portni ochadi (kameralar hodisa yuborishi uchun);
  - agentni kompyuter yonganda o'zi ishga tushadigan qiladi (Task Scheduler,
    "ParkingAgent", SYSTEM nomidan, to'xtab qolsa 1 daqiqada qayta ishga tushadi);
  - backendga ulanganini tekshiradi.

Admin panelda agent "ONLAYN" bo'lib ko'rinishi kerak.


4. SHLAGBAUMNI BOG'LASH
-----------------------
Admin panelda shlagbaumni tahrirlang -> "Buyruqni kim yuboradi?" -> shu agent.
Shlagbaumning IP manzili va logini parkovka tarmog'idagi qiymatlar bo'ladi
(masalan 192.168.1.64). "Tekshirish" tugmasi javobida "Agent orqali" chiqadi.


5. TEKSHIRISH VA LOG
--------------------
Holat:  brauzerda http://127.0.0.1:8090/health  ("connected": true)
Log:    C:\ProgramData\parking-agent\agent.log
Qo'lda: Task Scheduler -> ParkingAgent -> Run / End

Sozlamani o'zgartirsangiz: agent.env ni tahrirlang va install.cmd ni qayta
ishga tushiring (yoki Task Scheduler'da ParkingAgent -> End, keyin Run).

Kompyuter BIOS'ida "Restore on AC power loss = Power On" ni yoqing,
Windows'da uyqu rejimini o'chiring va kompyuterni UPS'ga ulang.


6. O'CHIRISH
------------
uninstall.cmd -> o'ng tugma -> "Run as administrator".
