# Shlagbaum uchun ESP8266 tarmoq relesi

Bu proshivka 2 kanalli ESP8266 rele platasini (masalan, "ESP-12F 2 channel relay") backend'ning `HTTP_RELAY` shlagbaum turiga moslaydi:

| Kanal | Kontrollerga ulanishi | Backend so'rovi | Vazifasi |
|---|---|---|---|
| 1 | `UP` + `G` | `POST /relay/1?token=…` | Ochish |
| 2 | `DOWN` + `G` | `POST /relay/2?token=…` | Yopish (avto-yopish taymeri ham shuni yuboradi) |

Har bir buyruq releni 1 soniya yopib turadi. Ikki kanal bir vaqtda hech qachon yopilmaydi: biriga buyruq kelsa, ikkinchisi darhol bo'shatiladi.

## 1. Proshivka yuklash

1. Arduino IDE'ga ESP8266 platasini qo'shing: File → Preferences → Additional boards manager URLs: `https://arduino.esp8266.com/stable/package_esp8266com_index.json`, keyin Boards Manager'dan "esp8266" ni o'rnating.
2. `config.example.h` faylini `config.h` nomi bilan nusxalang va to'ldiring: Wi-Fi nomi va paroli, qurilmaning doimiy IP manzili, uzun tasodifiy `RELAY_TOKEN`.
3. Board: "Generic ESP8266 Module" (yoki NodeMCU 1.0) ni tanlang va yuklang.
   Platada USB bo'lmasa, USB-TTL adapter kerak: `TX`/`RX`/`GND`, yuklash paytida `GPIO0` (`IO0`) ni `GND` ga ulab turish kerak.
4. Serial Monitor'ni (115200) oching: IP manzil va Wi-Fi signali (RSSI) chiqadi.

## 2. Stolda sinash

Rele platasini shlagbaumga ulamasdan, kompyuterdan:

```bash
curl -X POST "http://192.168.1.120/relay/1?token=SIZNING_TOKEN"
curl -X POST "http://192.168.1.120/relay/2?token=SIZNING_TOKEN"
curl "http://192.168.1.120/status?token=SIZNING_TOKEN"
```

Har bir buyruqda tegishli rele chiqillashi va 1 soniyadan keyin qaytishi kerak. Multimetr bilan `NO`–`COM` orasini tekshiring: impuls paytida qisqa tutashuv, qolgan vaqtda uzilish.

Agar rele teskari ishlasa (doim yopiq, buyruqda ochiladi), `config.h` da `RELAY_ACTIVE_HIGH 0` qiling. Boshqa rele chiqillasa, `RELAY_UP_PIN` / `RELAY_DOWN_PIN` ni almashtiring.

`/status` dagi `rssi` -75 dBm dan past bo'lsa (masalan -82), temir shkaf ichida Wi-Fi beqaror bo'ladi: antennani shkaf tashqarisiga chiqaring yoki routerni yaqinlashtiring.

## 3. Shlagbaumga ulash

Ulashdan oldin shlagbaum tokini o'chiring, shkafda 220V bor.

- 1-kanal `NO` → kontroller `UP`, `COM` → `G`
- 2-kanal `NO` → kontroller `DOWN`, `COM` → `G`

Pult qabul qiluvchisi ham shu klemmalarga ulangan, rele unga parallel ulanadi. Ikkalasi ham ishlayveradi.

Toki: alohida 5V yoki 12V manba ishlating (plataning kirish kuchlanishini tekshiring). Kontrollerning `+12V` klemmasi jami 100 mA beradi, ESP esa 70–200 mA yeydi.

## 4. Fotoelement

- Uzatgich (TX): `+12V` va `G`.
- Qabul qiluvchi (RX): `+12V` va `G` (tok), `NC` (yoki `NO`) va `COM` → kontroller `PHO` va `G`.

Kontrollerlar odatda `NC` kutadi: nur to'silganda kontakt uziladi. Ulagach sinang: strela tushayotganda nurni qo'l bilan to'sing, u to'xtashi yoki qayta ko'tarilishi kerak. To'xtamasa, `NO` ga o'tkazib ko'ring.

## 5. Admin panelda

Shlagbaumni tahrirlang:

- Qanday ochiladi: **HTTP relay kontroller**
- Ochish URL: `http://192.168.1.120/relay/1?token=SIZNING_TOKEN`
- Yopish URL: `http://192.168.1.120/relay/2?token=SIZNING_TOKEN`
- Avto-yopilish: mashina o'tib ulguradigan vaqt (masalan 10–15 soniya)

So'ng **Tekshirish** (aloqa) va **Ochish** tugmalarini bosing. Avto-yopilish vaqtidan keyin backend yopish buyrug'ini o'zi yuboradi.
