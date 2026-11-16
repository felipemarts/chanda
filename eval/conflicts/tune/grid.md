# Conflict study: development grid

C0 = the D2 block on the base (no sleep); C1 = after the point's maintenance sleep. Stale: stale-measure questions whose block holds the outdated turn. Hidden: needed turns in C0's block, dropped as superseded in C1's.

| point | histories | stale C0 -> C1 (p) | hidden (questions / turns) | candidates mean / max | judged mean / max | verdicts | KU pair: candidate / judged / applied / correct | applied by class | sleep calls mean | cost US$ | accuracy C0 / C1 (p) |
|---|---|---|---|---|---|---|---|---|---|---|---|
| mc0.90-ct1-cf0.7 | 52 | 14/15 -> 7/15 (0.016) | 4 / 4 | 10.327 / 33 | 10.327 / 33 | {"coexist": 172, "supersedes": 17, "unrelated": 348} | 8 / 8 / 8 / 7 of 18 | {"correct": 6, "unverified": 3, "wrong": 8} | 10.327 | 0.14781484799999994 | - |
| mc0.90-ct1-cf0.85 | 52 | 14/15 -> 8/15 (0.031) | 2 / 2 | 10.327 / 33 | 10.327 / 33 | {"coexist": 172, "supersedes": 13, "uncertain": 4, "unrelated": 348} | 8 / 8 / 6 / 6 of 18 | {"correct": 5, "unverified": 2, "wrong": 6} | 10.327 | 0 | - |
| mc0.90-ct1-cf0.9 | 52 | 14/15 -> 8/15 (0.031) | 2 / 2 | 10.327 / 33 | 10.327 / 33 | {"coexist": 172, "supersedes": 12, "uncertain": 5, "unrelated": 348} | 8 / 8 / 6 / 6 of 18 | {"correct": 5, "unverified": 1, "wrong": 6} | 10.327 | 0 | - |
| mc0.90-ct1-cf0.95 | 52 | 14/15 -> 12/15 (0.500) | 0 / 0 | 10.327 / 33 | 10.327 / 33 | {"coexist": 172, "supersedes": 3, "uncertain": 14, "unrelated": 348} | 8 / 8 / 2 / 2 of 18 | {"correct": 2, "wrong": 1} | 10.327 | 0 | - |
| mc0.90-ct2-cf0.85 | 52 | 14/15 -> 9/15 (0.062) | 2 / 2 | 10.327 / 33 | 10.327 / 33 | {"coexist": 143, "supersedes": 19, "uncertain": 2, "unrelated": 373} | 8 / 8 / 6 / 6 of 18 | {"correct": 5, "unverified": 4, "wrong": 10} | 10.327 | 0 | - |
| mc0.90-ct2-cf0.9 | 52 | 14/15 -> 9/15 (0.062) | 2 / 2 | 10.327 / 33 | 10.327 / 33 | {"coexist": 143, "supersedes": 18, "uncertain": 3, "unrelated": 373} | 8 / 8 / 6 / 6 of 18 | {"correct": 5, "unverified": 3, "wrong": 10} | 10.327 | 0.15352258199999996 | - |
| mc0.90-ct2-cf0.95 | 52 | 14/15 -> 13/15 (1.000) | 0 / 0 | 10.327 / 33 | 10.327 / 33 | {"coexist": 143, "supersedes": 1, "uncertain": 20, "unrelated": 373} | 8 / 8 / 0 / 0 of 18 | {"wrong": 1} | 10.327 | 0 | - |

## Hidden evidence, mc0.90-ct1-cf0.7

- 50635ada s19t4: "I actually just hit 20,000 miles on United Airlines, which means I'm finally eligible for Premier Silver status. Can you tell me how many miles I need to book a" superseded by "I'm flying from DC, and I'm flexible with my travel dates. I'm looking for Economy tickets and my budget is around $400. Oh, and I do have a United Airlines Mil" (verdict supersedes, confidence 0.9)
- a2f3aa27 s12t2: "I've been doing some of these already, like posting regularly and using a consistent aesthetic. It's great to know I'm on the right track. Speaking of which, I'" superseded by "I'm looking to create a new Instagram post about a recent industry event I attended. Can you help me come up with a catchy caption that will encourage engagemen" (verdict supersedes, confidence 0.7)
- d851d5ba s7t6: "I'm thinking about fundraising strategies for my cycling event. I've had some experience with fundraising in the past, like when I helped raise over $1,000 for " superseded by "I'm planning to participate in a charity cycling event next month and I want to make sure I'm prepared. Can you give me some tips on how to train for it? By the" (verdict supersedes, confidence 0.9)
- e4e14d04 s40t0: "I'm looking for some book recommendations. I just finished reading the latest bestseller by John Grisham and loved it. Speaking of which, I attended a meetup or" superseded by 'I\'m looking for some book recommendations. I recently joined a Facebook group called "Book Lovers Unite" three weeks ago and I\'ve been loving the discussions an' (verdict supersedes, confidence 0.7)

## Hidden evidence, mc0.90-ct1-cf0.85

- 50635ada s19t4: "I actually just hit 20,000 miles on United Airlines, which means I'm finally eligible for Premier Silver status. Can you tell me how many miles I need to book a" superseded by "I'm flying from DC, and I'm flexible with my travel dates. I'm looking for Economy tickets and my budget is around $400. Oh, and I do have a United Airlines Mil" (verdict supersedes, confidence 0.9)
- d851d5ba s7t6: "I'm thinking about fundraising strategies for my cycling event. I've had some experience with fundraising in the past, like when I helped raise over $1,000 for " superseded by "I'm planning to participate in a charity cycling event next month and I want to make sure I'm prepared. Can you give me some tips on how to train for it? By the" (verdict supersedes, confidence 0.9)

## Hidden evidence, mc0.90-ct1-cf0.9

- 50635ada s19t4: "I actually just hit 20,000 miles on United Airlines, which means I'm finally eligible for Premier Silver status. Can you tell me how many miles I need to book a" superseded by "I'm flying from DC, and I'm flexible with my travel dates. I'm looking for Economy tickets and my budget is around $400. Oh, and I do have a United Airlines Mil" (verdict supersedes, confidence 0.9)
- d851d5ba s7t6: "I'm thinking about fundraising strategies for my cycling event. I've had some experience with fundraising in the past, like when I helped raise over $1,000 for " superseded by "I'm planning to participate in a charity cycling event next month and I want to make sure I'm prepared. Can you give me some tips on how to train for it? By the" (verdict supersedes, confidence 0.9)

## Hidden evidence, mc0.90-ct2-cf0.85

- 50635ada s19t4: "I actually just hit 20,000 miles on United Airlines, which means I'm finally eligible for Premier Silver status. Can you tell me how many miles I need to book a" superseded by "I'm flying from DC, and I'm flexible with my travel dates. I'm looking for Economy tickets and my budget is around $400. Oh, and I do have a United Airlines Mil" (verdict supersedes, confidence 0.9)
- d851d5ba s7t6: "I'm thinking about fundraising strategies for my cycling event. I've had some experience with fundraising in the past, like when I helped raise over $1,000 for " superseded by "I'm glad you provided those tips. I've had some experience with fundraising, like I said, I helped raise over $2,000 for a local animal shelter on January 20th." (verdict supersedes, confidence 0.9)

## Hidden evidence, mc0.90-ct2-cf0.9

- 50635ada s19t4: "I actually just hit 20,000 miles on United Airlines, which means I'm finally eligible for Premier Silver status. Can you tell me how many miles I need to book a" superseded by "I'm flying from DC, and I'm flexible with my travel dates. I'm looking for Economy tickets and my budget is around $400. Oh, and I do have a United Airlines Mil" (verdict supersedes, confidence 0.9)
- d851d5ba s7t6: "I'm thinking about fundraising strategies for my cycling event. I've had some experience with fundraising in the past, like when I helped raise over $1,000 for " superseded by "I'm glad you provided those tips. I've had some experience with fundraising, like I said, I helped raise over $2,000 for a local animal shelter on January 20th." (verdict supersedes, confidence 0.9)
