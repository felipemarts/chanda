# Conflict study: Portuguese suite (e5)

40 pairs.

- Updates: detected 16/20; verdicts of the detected pairs {"supersedes": 15, "uncertain": 1}; applied 15/20; other (wrong) supersessions in 0 pairs.
- Controls: detected 11/20; verdicts {"coexist": 8, "unrelated": 3}; **false supersessions 0/20** (stop above 2).
- Sleep: 27 calls, 14573+610 tokens, US$ 0.00189339 (tag conflicts-suite).

| pair | kind | category | detected | cosine | verdict | confidence | reason | applied | false/other |
|---|---|---|---|---|---|---|---|---|---|
| u01 | update | endereco | no | - | - | - | - | no | no |
| u02 | update | endereco | no | - | - | - | - | no | no |
| u03 | update | endereco | yes | 0.934 | supersedes | 0.97 | applied | yes | no |
| u04 | update | cargo | no | - | - | - | - | no | no |
| u05 | update | cargo | yes | 0.921 | supersedes | 0.95 | applied | yes | no |
| u06 | update | cargo | yes | 0.933 | supersedes | 0.97 | applied | yes | no |
| u07 | update | preco | yes | 0.955 | supersedes | 0.98 | applied | yes | no |
| u08 | update | preco | yes | 0.946 | supersedes | 0.95 | applied | yes | no |
| u09 | update | preco | yes | 0.905 | supersedes | 0.97 | applied | yes | no |
| u10 | update | preferencia | yes | 0.917 | supersedes | 0.98 | applied | yes | no |
| u11 | update | preferencia | yes | 0.938 | supersedes | 0.97 | applied | yes | no |
| u12 | update | preferencia | yes | 0.908 | supersedes | 0.98 | applied | yes | no |
| u13 | update | projeto | no | - | - | - | - | no | no |
| u14 | update | projeto | yes | 0.914 | supersedes | 0.95 | applied | yes | no |
| u15 | update | projeto | yes | 0.915 | supersedes | 0.95 | applied | yes | no |
| u16 | update | projeto | yes | 0.925 | supersedes | 0.95 | applied | yes | no |
| u17 | update | quantidade | yes | 0.913 | supersedes | 0.95 | applied | yes | no |
| u18 | update | quantidade | yes | 0.929 | supersedes | 0.95 | applied | yes | no |
| u19 | update | quantidade | yes | 0.916 | supersedes | 0.95 | applied | yes | no |
| u20 | update | quantidade | yes | 0.919 | uncertain | 0.85 | uncertain:low_confidence | no | no |
| c01 | control | outra_pessoa | no | - | - | - | - | no | no |
| c02 | control | outra_pessoa | yes | 0.949 | unrelated | 0.95 | unrelated | no | no |
| c03 | control | outra_pessoa | yes | 0.931 | unrelated | 0.85 | unrelated | no | no |
| c04 | control | outra_pessoa | yes | 0.919 | unrelated | 0.7 | unrelated | no | no |
| c05 | control | outro_atributo | yes | 0.915 | coexist | 0.9 | coexist | no | no |
| c06 | control | outro_atributo | yes | 0.919 | coexist | 0.95 | coexist | no | no |
| c07 | control | outro_atributo | yes | 0.958 | coexist | 0.9 | coexist | no | no |
| c08 | control | soma | no | - | - | - | - | no | no |
| c09 | control | soma | no | - | - | - | - | no | no |
| c10 | control | soma | yes | 0.904 | coexist | 0.95 | coexist | no | no |
| c11 | control | soma | yes | 0.921 | coexist | 0.9 | coexist | no | no |
| c12 | control | historico | yes | 0.912 | coexist | 0.9 | coexist | no | no |
| c13 | control | historico | no | - | - | - | - | no | no |
| c14 | control | historico | yes | 0.914 | coexist | 0.9 | coexist | no | no |
| c15 | control | hipotese | no | - | - | - | - | no | no |
| c16 | control | hipotese | yes | 0.924 | coexist | 0.8 | coexist | no | no |
| c17 | control | hipotese | no | - | - | - | - | no | no |
| c18 | control | citacao | no | - | - | - | - | no | no |
| c19 | control | citacao | no | - | - | - | - | no | no |
| c20 | control | citacao | no | - | - | - | - | no | no |
