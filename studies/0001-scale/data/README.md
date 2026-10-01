# Raw data of the scale measurements

These are the outputs that `../11-scale.md` reads its numbers from, as the runs
wrote them on 2026-09-30, with one change. Each benchmark output's `cpu:` line
named the processor's product, which no document of this tree names
(AGENTS.md); it reads here `cpu: [product name removed] Processor @ 2.10GHz`.
Every other byte is as written. The SHA-256 of each file as written and as kept
here is listed, so a copy of the originals kept outside the tree can be checked
against this index.

Every run: one host with four virtual processors at 2.10 GHz, 15.7 GiB of
memory and one virtual disk (ext4), Linux 6.18, Go 1.26.4 linux/amd64, run as
the superuser, one measurement at a time. Short checks ran beside two of them:
beside the sparse vessel (`vessel.txt`), between about 18:52 and 19:01, a probe
of the revocation sets of about a second, three smoke runs of new benchmarks of
a few seconds each and one of the readers of 12 s, all while the vessels of
256 GiB and 1 TiB were measured; and beside the second run of the writers
(`writers2.txt`), at 19:20:36 to 19:20:55, three compilations of the bench
package for commits. The production code of every run is that of commit 4111458
(the release, ba7e695, with the repair of `ledger.Load`); every later commit
changes only benchmarks and documents, which
`git diff --stat 4111458 HEAD -- . ':(exclude)*_test.go' ':(exclude)*.md'`
shows by printing nothing.

"Benchmark code" names the commit that holds the benchmark as it ran. The first
round (chain to vessel on the disk) started at 18:37:45 from the working tree,
a few minutes before that tree's benchmarks were committed as a72bc74 and
b2c436a at 18:43; the files it compiled were last changed before the round
began, but for `bench/scale_ledger_test.go`, whose later changes (the order of
revocations at 18:44, the seeds in one ledger at about 18:58) are the reason
`revokes.txt` and `seedledger.txt` exist. Times are UTC.

| file | what | command, in `rokh/` | benchmark code | ran |
|---|---|---|---|---|
| `chain.txt` | `BenchmarkScaleChain` | `go test ./bench -run '^$' -bench 'BenchmarkScaleChain' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:37:45–18:42:54 |
| `authority.txt` | `BenchmarkScaleAuthority`: grants and keyring; its revocation rows are superseded by `revokes.txt` | `go test ./bench -run '^$' -bench 'BenchmarkScaleAuthority' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:42:54–18:43:23 |
| `envelope.txt` | `BenchmarkScaleEnvelope` | `go test ./bench -run '^$' -bench 'BenchmarkScaleEnvelope' -benchtime 300x -timeout 0 -v` | a72bc74 | 18:43:23–18:43:24 |
| `passphrase.txt` | `BenchmarkScalePassphrase` | `go test ./bench -run '^$' -bench 'BenchmarkScalePassphrase' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:43:24–18:43:25 |
| `leaf.txt` | `BenchmarkScaleLeaf` | `go test ./bench -run '^$' -bench 'BenchmarkScaleLeaf' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:43:25–18:43:32 |
| `seeds.txt` | `BenchmarkScaleSeeds` (command line) | `go test ./cmd/rokh -run '^$' -bench 'BenchmarkScaleSeeds' -benchtime 1x -timeout 0 -v` | b2c436a | 18:43:32–18:44:42 |
| `writers.txt` | `BenchmarkScaleWriters`, the first nine configurations | `go test ./bench -run '^$' -bench 'BenchmarkScaleWriters' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:44:42–18:45:38 |
| `door.txt` | `BenchmarkScaleDoor` | `go test ./bench -run '^$' -bench 'BenchmarkScaleDoor' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:45:38–18:48:56 |
| `vessel.txt` | `BenchmarkScaleVessel` (the sparse medium) | `go test ./bench -run '^$' -bench 'BenchmarkScaleVessel$' -benchtime 1x -timeout 0 -v` | a72bc74 | 18:48:56–19:04:05 |
| `vesseldisk.txt` | `BenchmarkScaleVesselDisk` | `go test ./bench -run '^$' -bench 'BenchmarkScaleVesselDisk' -benchtime 1x -timeout 0 -v` | a72bc74 | 19:04:05–19:09:41 |
| `revokes.txt` | `BenchmarkScaleAuthority/revokes`, taken back newest first | `go test ./bench -run '^$' -bench 'BenchmarkScaleAuthority/revokes' -benchtime 1x -timeout 0 -v` | f459779 | 19:09:44–19:10:01 |
| `openaddress.txt` | `BenchmarkScaleOpenAddress` | `go test ./bench -run '^$' -bench 'BenchmarkScaleOpenAddress' -benchtime 1x -timeout 0 -v` | a72bc74 | 19:10:01–19:13:49 |
| `seedledger.txt` | `BenchmarkScaleSeedLedger` | `go test ./bench -run '^$' -bench 'BenchmarkScaleSeedLedger' -benchtime 1x -timeout 0 -v` | 6e65793 | 19:13:49–19:14:18 |
| `reconcile.txt` | `BenchmarkScaleReconcile` | `go test ./bench -run '^$' -bench 'BenchmarkScaleReconcile' -benchtime 1x -timeout 0 -v` | a72bc74 | 19:14:18–19:16:25 |
| `content.txt` | `BenchmarkScaleContent` | `go test ./bench -run '^$' -bench 'BenchmarkScaleContent' -benchtime 1x -timeout 0 -v` | 617ce7b | 19:16:25–19:19:36 |
| `readers.txt` | `BenchmarkScaleReaders` | `go test ./bench -run '^$' -bench 'BenchmarkScaleReaders' -benchtime 1x -timeout 0 -v` | 91f6101 | 19:19:36–19:20:26 |
| `writers2.txt` | `BenchmarkScaleWriters`, all twelve configurations | `go test ./bench -run '^$' -bench 'BenchmarkScaleWriters' -benchtime 1x -timeout 0 -v` | 91f6101 | 19:20:26–19:22:24 |
| `door-profile.txt` | `BenchmarkScaleDoor/events=100000` under the processor profile, summarized for the door's `Handle` | `go test ./bench -run '^$' -bench 'BenchmarkScaleDoor/events=100000$' -benchtime 1x -timeout 0 -cpuprofile cpu.out`, then `go tool pprof -top -cum -focus='daemon.\(\*Server\).Handle' bench.test cpu.out` | 91f6101 | 19:24:38–19:25:25 |
| `progress.txt` | the start and end of every run above but the profile, as the runner wrote them |  |  |  |
| `suite-rokh.txt` | the whole suite of the core, as the superuser | `go test -timeout 45m ./...` | 91f6101 | about 19:28–19:37 |
| `suite-home.txt` | the whole suite of the home, as the superuser | `go test ./...` in `rokh-home` | 91f6101 | about 19:37 |

Kept outside the tree, with the person who asked for these measurements: the
unedited outputs; the processor profile itself (`cpu.out`, SHA-256 `07c27f4b…`)
and the test binary that reads it; and the output of a probe, never part of the
tree, that loaded chains of 500,000, 650,000 and 1,000,000 events with the walk
as it was before 4111458 (the first two loaded, the third ended the process
with a stack overflow after a walk 645,365 generations deep).
`ledger/deep_test.go` shows the same fault on the old walk at a small scale.

Not kept anywhere: a log of the run of `go test ./cmd/rokh` by an unprivileged
account (uid 65534) on the tree of 91f6101, which ended
`ok rokh/cmd/rokh 518.347s`, and of the two runs, as the superuser and as that
account, of the two tests named in STATE.md on the release commit; their
outcome is written in `../11-scale.md`, section T, from the session's own
reading of the output.

| file | bytes as written | SHA-256 as written | SHA-256 here |
|---|---|---|---|
| `chain.txt` | 1634 | `a080df7b818f4696f562daa9d771a43ec9fa3ac6548962429a3e2a851fc8c2cc` | `6c34ddb41657cdab259bc6ac411f1b545e6672886c187f87a37e378190d90f8e` |
| `authority.txt` | 4188 | `f8d2518875043ef0db2fc6721df3a5a7f7bfc77927a40d57e3a9ffa4826c9613` | `dc5872df4ba2aec2b1f6efc05651f7e2aa64bcd01ffc0704d35a3ea2a12406cb` |
| `envelope.txt` | 1413 | `b59a02d031baf1abd598001f243d949fec6161d99be8fc34f0129a948e5f5813` | `0003a8ccbcb7150a2f543938223cfb669733223af7b9634b1982719118cad814` |
| `passphrase.txt` | 245 | `54d8a59dcfd7dae892351e02e7a70548b41b49571fb9b1ee9a19be2bc4db67b0` | `048691829576db5977b0a1f40a428c72a1f68fd929cb4c496fe155a1196d5a8b` |
| `leaf.txt` | 908 | `52864e38481fa0b5cbc43f72a562e25bc9f31c5432c80606953e34dfb884ac9a` | `2c0aa9b41544d619a0db46b0b9c1abb89bfe34794b9558fa7a9cea6f1a3bb0cb` |
| `seeds.txt` | 5441 | `a2dadba87b951b1fb6e7c148b1c01d33d32265cb8f15ea298dc970fe2e9fb5e5` | `db0d85528b659aa027ed2a3c788ef628e3bdd19aee29ce3184cdc886303473f1` |
| `writers.txt` | 2326 | `da6cbdf8c942932ee44c0bcb1dd10da25e25d32283ac223e77ba7ee13a098694` | `596179bea4d91c1508e407d5bd86551064aeedf900bf3d3882374f1f14c03b46` |
| `door.txt` | 1214 | `44aaf721f658fe65a5e7e4d2ecad327096b1114bc01a71d794cc87ce34e76e7a` | `ab429db177b092f22a32b805816f1bb7c8e3cfd9c77a39e543ba02501bbcda9c` |
| `vessel.txt` | 6413 | `474a8c5ccbad6527c7025c294b607a9475bd57ac8a9cbe3e5c68617df983dc9b` | `3a0a867d826bf6b3c061bd63a1bf7deab9e3164f616f41c5bb02aa194c419f0a` |
| `vesseldisk.txt` | 2042 | `9dffb334cf43f61dbde3d48f7e3d437ec7901a0363d796c46a4ec8f9e71b3204` | `37bab89b9758338e1ce25c85b28f418bcc0783cc215be00cd8d43989af57da7b` |
| `revokes.txt` | 1490 | `d376f5aeb7a406f343a568cbb901e4b26361a850193ccd0031e19c073f5803e1` | `7a7586d19d84b5bcdfa6c5178795926efddfd871eb65baf05f1f557059bd297a` |
| `openaddress.txt` | 1130 | `f971c36fd3daf0d17e0ec09519ce581c2520c698cfb27b72faafe526e5542e11` | `33a6ded095587788f99d5658c283d1bfce72c6e2cc24667dd3dee81e413ba924` |
| `seedledger.txt` | 2555 | `47a9a6070baef3866c251bf6cacca42c4387302ffa753b3098797676649405a0` | `65d9587ed20430bef0c653548396b5603e3998c28a55f953bf26f2f84b838585` |
| `reconcile.txt` | 795 | `06758ed58836fa868970c41d88dfa05b74c34262898f745dc630fdf00f1558b6` | `6d2496669037404f33ee58fde3e700b248b9e2d21538e1ac20c8d9719e0ee6a7` |
| `content.txt` | 1661 | `8a0a29f57b9ca1f5fb2b61771c68948564fe637a7a6383ef8a674f3cf03aca14` | `47cba142fe7e72cede755f680833b5b6602fb4d82a0330e6c5287d5718da9229` |
| `readers.txt` | 1589 | `7446ef77b37c4c6c87a75700fdf06f2876ead8017d5a2381ddbc93359ce542da` | `54b987da852ef56cf8a9de6caafa1ebc456dd43a3692903f6fdcd3931b0f559d` |
| `writers2.txt` | 3129 | `ebc55b8394a343d843af545da50751f1dbf6a1a3026f26a8fe7cee26ecd1162c` | `e80835840d245bb3d3738c9bcf60c330166699f4cadb4717d68e46e53e4b7d5e` |
| `door-profile.txt` | 7433 | `b2492f42142bd351e197a7e12f034499b8465502c56fb1ab27f3da384b5ae656` | the same |
| `progress.txt` | 719 | `ef0ee3b3c5c805d2d07402278514b40a7512988b4bddaee49d09ec25660f44da` | the same |
| `suite-rokh.txt` | 1432 | `6b0b4b57b67227968f1ba456f96589fea1aefb7b3ef7ed9bcd7029934519ea89` | the same |
| `suite-home.txt` | 325 | `ab4abf064cac1125ae4c9ce70af54b3bcc78223af1dc36f9283fc7a793f7f218` | the same |
