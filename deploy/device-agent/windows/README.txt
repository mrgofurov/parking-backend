SMART PARKING DEVICE-AGENT (WINDOWS)
====================================

Parkovkadagi Windows kompyuterga o'rnatiladi. Agent https://apipark.duckdns.org
ga o'zi ulanadi, shlagbaum buyruqlarini lokal tarmoqda bajaradi va kamera
hodisalarini bulutga uzatadi. Parkovkada port ochish yoki statik IP kerak emas.

Talablar: Windows 10/11 yoki Windows Server 2016+, 64-bit, internet.


1. O'RNATISH (ENG OSON YO'L: BITTA FAYL, HECH NARSA YOZILMAYDI)
---------------------------------------------------------------
Parkovkadagi kompyuterning o'zida (touch monitor bo'lsa ham bo'ladi):

  a) Brauzerda admin panelni oching: https://adminpark.duckdns.org
     Kamera & Shlagbaum -> Device-agentlar.
     Agent yo'q bo'lsa "Agent qo'shish" (nomini yozing) -> chiqqan oynada
     "Windows o'rnatuvchini yuklab olish".
     Agent bor bo'lsa uning kartochkasida "Windows o'rnatuvchi".
  b) Yuklangan parking-agent-setup.exe ni ikki marta bosing.
     Windows "Windows protected your PC" desa: "More info / Batafsil" ->
     "Run anyway / Baribir ishga tushirish". Keyin "Ha / Yes".
  c) "Tayyor!" oynasi chiqadi. Admin panelda agent "ONLAYN" bo'ladi.

Faylning ichida backend manzili, agentning yangi tokeni va ANPR tokeni bor:
agent.env yaratish yoki Notepad'da yozish kerak emas. Har yuklab olishda
yangi token chiqadi va oldingisi bekor bo'ladi, shuning uchun eski
o'rnatuvchi fayllarni saqlab qo'ymang (ichida token bor).

O'rnatuvchi:
  - agentni C:\Program Files\ParkingAgent ga ko'chiradi, agent.env ni
    faqat Administrator va SYSTEM o'qiy oladigan qilib yozadi;
  - Windows Firewall'da 8090-portni ochadi (kameralar hodisa yuborishi uchun);
  - agentni kompyuter yonganda o'zi ishga tushadigan qiladi (Task Scheduler,
    "ParkingAgent", SYSTEM nomidan, to'xtab qolsa 1 daqiqada qayta ishga tushadi);
  - backendga ulanganini tekshiradi.

Qayta o'rnatish yoki tokenni almashtirish: yangi o'rnatuvchini yuklab oling
va yana ikki marta bosing.


2. QO'LDA O'RNATISH (zip papkadan)
----------------------------------
  - Admin panelda token oling ("Agent qo'shish" yoki "Yangi token").
  - agent.env.example ni agent.env nomi bilan nusxalang, Notepad'da
    AGENT_TOKEN va AGENT_ANPR_TOKEN (serverdagi ANPR_WEBHOOK_TOKEN) ni yozing.
  - install.cmd -> o'ng tugma -> "Run as administrator".


3. ADMIN PANELDA
----------------
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

Sozlamani o'zgartirsangiz: admin paneldan yangi o'rnatuvchini yuklab olib
ishga tushiring (yoki C:\Program Files\ParkingAgent\agent.env ni tahrirlang va
Task Scheduler'da ParkingAgent -> End, keyin Run).

Kompyuter BIOS'ida "Restore on AC power loss = Power On" ni yoqing,
Windows'da uyqu rejimini o'chiring va kompyuterni UPS'ga ulang.


6. O'CHIRISH
------------
C:\Program Files\ParkingAgent\uninstall.cmd -> o'ng tugma -> "Run as administrator".
