# Conflict study, Part 0 (development set, no LLM)

52 development histories (missing: none). Detection at write with `sleep.conflicts.enabled=true`; `conflicts scan -details` (no write) at each min_cosine.

## Candidates and near-duplicate refusals per history (mean / median / max)

| min_cosine | KU candidates | other candidates | KU refusals | other refusals | qualifying KU: pair is a candidate | refusal links the pair | same event |
|---|---|---|---|---|---|---|---|
| 0.70 | 1362.6 / 1358.5 / 1479 | 1369.9 / 1344 / 1553 | 1.3 / 1 / 4 | 2.1 / 1.5 / 11 | 10/18 | 0/18 | 0/18 |
| 0.75 | 1344.1 / 1336 / 1461 | 1350.1 / 1324 / 1533 | 1.3 / 1 / 4 | 2.1 / 1.5 / 11 | 10/18 | 0/18 | 0/18 |
| 0.80 | 1074.3 / 1089 / 1155 | 1097.0 / 1092 / 1261 | 1.3 / 1 / 4 | 2.1 / 1.5 / 11 | 10/18 | 0/18 | 0/18 |
| 0.85 | 363.2 / 352 / 482 | 375.1 / 353 / 550 | 1.3 / 1 / 4 | 2.1 / 1.5 / 11 | 10/18 | 0/18 | 0/18 |

Refusals by reason (totals):

- 0.70: KU {"entities_differ": 8, "negation_differs": 1, "numbers_differ": 20}; other {"entities_differ": 16, "negation_differs": 9, "numbers_differ": 37}
- 0.75: KU {"entities_differ": 8, "negation_differs": 1, "numbers_differ": 20}; other {"entities_differ": 16, "negation_differs": 9, "numbers_differ": 37}
- 0.80: KU {"entities_differ": 8, "negation_differs": 1, "numbers_differ": 20}; other {"entities_differ": 16, "negation_differs": 9, "numbers_differ": 37}
- 0.85: KU {"entities_differ": 8, "negation_differs": 1, "numbers_differ": 20}; other {"entities_differ": 16, "negation_differs": 9, "numbers_differ": 37}

## D2 blocks on the base (item 1)

Stale-measure questions: 15. Blocks holding the outdated turn: 14; holding it without the current one: 1; holding the current one: 14.

## Knowledge-update questions

| question | qualifies | stale measure | same day | same event | 0.70: cand / pair / refusals (link) | 0.75: cand / pair / refusals (link) | 0.80: cand / pair / refusals (link) | 0.85: cand / pair / refusals (link) | D2 outdated / current |
|---|---|---|---|---|---|---|---|---|---|
| 01493427 | yes | yes | no | no | 1352 / yes / 0 | 1334 / yes / 0 | 1081 / yes / 0 | 352 / yes / 0 | yes / yes |
| 22d2cb42 | no | no | no | no | 1314 / - / 1 | 1306 / - / 1 | 1054 / - / 1 | 328 / - / 1 | - |
| 2698e78f | yes | yes | no | no | 1337 / no / 0 | 1330 / no / 0 | 1132 / no / 0 | 482 / no / 0 | yes / yes |
| 2698e78f_abs | no | no | no | no | 1284 / - / 1 | 1269 / - / 1 | 1058 / - / 1 | 437 / - / 1 | - |
| 26bdc477 | yes | yes | no | no | 1381 / no / 1 | 1353 / no / 1 | 1088 / no / 1 | 420 / no / 1 | yes / yes |
| 41698283 | yes | yes | no | no | 1306 / no / 1 | 1283 / no / 1 | 1003 / no / 1 | 352 / no / 1 | yes / no |
| 4b24c848 | yes | yes | no | no | 1410 / no / 0 | 1388 / no / 0 | 1108 / no / 0 | 434 / no / 0 | yes / yes |
| 4d6b87c8 | yes | yes | no | no | 1438 / no / 0 | 1409 / no / 0 | 1124 / no / 0 | 329 / no / 0 | yes / yes |
| 50635ada | yes | no | no | no | 1448 / yes / 1 | 1418 / yes / 1 | 1140 / yes / 1 | 352 / yes / 1 | yes / yes |
| 6071bd76 | yes | yes | no | no | 1333 / yes / 0 | 1329 / yes / 0 | 1131 / yes / 0 | 392 / yes / 0 | yes / yes |
| 6a27ffc2 | yes | yes | no | no | 1366 / yes / 4 | 1338 / yes / 4 | 975 / yes / 4 | 255 / yes / 4 | yes / yes |
| 6aeb4375_abs | no | no | no | no | 1453 / - / 0 | 1438 / - / 0 | 1155 / - / 0 | 327 / - / 0 | - |
| 7401057b | yes | yes | no | no | 1224 / no / 0 | 1211 / no / 0 | 968 / no / 0 | 316 / no / 0 | yes / yes |
| 7a87bd0c | yes | yes | no | no | 1369 / yes / 4 | 1340 / yes / 4 | 1053 / yes / 4 | 328 / yes / 4 | yes / yes |
| 8fb83627 | no | no | no | no | 1340 / - / 2 | 1326 / - / 2 | 1068 / - / 2 | 321 / - / 2 | - |
| 945e3d21 | yes | yes | no | no | 1354 / no / 0 | 1333 / no / 0 | 1103 / no / 0 | 424 / no / 0 | no / yes |
| 9ea5eabc | yes | yes | no | no | 1479 / yes / 2 | 1461 / yes / 2 | 1142 / yes / 2 | 422 / yes / 2 | yes / yes |
| a2f3aa27 | yes | no | yes | no | 1240 / yes / 0 | 1231 / yes / 0 | 974 / yes / 0 | 300 / yes / 0 | yes / yes |
| b6019101 | yes | yes | no | no | 1412 / yes / 1 | 1390 / yes / 1 | 1094 / yes / 1 | 360 / yes / 1 | yes / yes |
| c4ea545c | yes | no | no | no | 1323 / no / 3 | 1295 / no / 3 | 967 / no / 3 | 355 / no / 3 | yes / yes |
| cc5ded98 | yes | yes | no | no | 1363 / yes / 4 | 1350 / yes / 4 | 1090 / yes / 4 | 378 / yes / 4 | yes / yes |
| d7c942c3 | yes | yes | no | no | 1452 / yes / 4 | 1439 / yes / 4 | 1127 / yes / 4 | 326 / yes / 4 | yes / yes |
