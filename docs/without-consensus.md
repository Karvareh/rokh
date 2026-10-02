# The ledger without consensus

*Rokh — the ledger of a person's events: without consensus, without network, without token.*

*This document is the founding text, not a report of the code. **The design rulings bind every implementation**; the base profile, the state in section 9.5 and the conjectures in section 8 are reports, not requirements. The code is a provisional witness and is checked against the rulings, not the reverse.*

## Abstract

Every person today has a ledger that is not their own. What they have done, what they have said and what they have made is recorded in a system that can rewrite it, close their access to it, or read it without permission. The existing solutions have solved this with **consensus**: a shared ledger whose correctness a majority votes on.

We show that for the ledger of one person, consensus is not needed — and **the problem consensus was built to solve does not arise at all in a person's ledger.** Rokh is a single-owner, hash-chained, signed ledger, all of which one person can verify alone, without connection to any network. Authority in it is granted and revoked, and its ruling is judged only in the causal past. Disclosure is separate from authorship.

The result is a ledger that needs no token, no miner, no live network, no one's permission — and if all the machines of the world are switched off, what is written in it can still be verified.


## Section 1 · The problem — a ledger that is not yours

**1.1** Every day, something you do is recorded somewhere: a message, a payment, a file, a treatment, a contract, a command to an AI system.

**1.2** You are not the one who records.

**1.3** So three powers have reached the recorder that have not reached you: **rewriting** — it can change the line; **closing** — it can close the door; **reading** — it can read without asking.

**1.4** None of these three is malice; all three are *architecture*. That system, even with the best of intentions, has these powers, and a power that exists is one day used.


> ### This is not a problem of privacy; it is a problem of ownership.

**1.5** Privacy means "no one sees". Ownership means **"I decide who sees."** Encryption solves the first; Rokh, the second.

**1.6** And a new problem has been added: when a machine works on your behalf, it must be possible to ask "who did this, on whose authority, and where is its receipt?" Today the answer to this question is in the log of a program that can itself rewrite it. That is, there is no answer.


## Section 2 · Why consensus is not needed

**2.1** Consensus was built for one specific problem and solved it well: **double spending**. If two people can spend one coin at the same time, someone must say which is the right one. In the absence of an arbiter, that someone became "the majority".

**2.2** But this solution has a price that is less often said: *for my history to be right, others must vote.* The network must be alive. The majority must stay sound. And writing must cost something so that voting stays expensive.

**2.3** In a person's ledger, that problem **does not exist**. My ledger has nothing for two people to fight over. An event is not spent; it is added. Two copies of my ledger do not compete with each other — they add together.

**2.4** And this is not a philosophical claim, it is a structural result: because every event names its parents and its name is its own hash, **the union of two copies is commutative and rewrites nothing** — no one needs to say which branch wins. But this concerns bytes, not meaning: two concurrent acts can fight with each other in the world, and resolving that is the work of the application layer.

**2.5** So Rokh has no consensus, and needs none. And because it has none, it does not have these either: miner, token, fee, live network, and that unpleasant rule that **a majority votes on your history**.

**2.6** What replaces consensus? Nothing. **Verification replaces voting.** One person, offline, with a small program, verifies the whole ledger: the bytes against their hash, the hash against its signature, and the signature against the authority valid in the event's causal past. If it checks out, it checks out.


> ### Mathematics does not vote for the majority.

**2.7** A subtle distinction that must not be lost: consensus answers a real question that Rokh does not ask at all — "among several incompatible accounts, which is the *common history*?" Rokh does not make a common history. Every ledger is the history of one person. Being common, if it is ever needed, is the work of a higher layer — not a condition for the ledger to work.

**2.8** And now let us state the boundary of the claim precisely, since a loose claim is as much an error as a wrong one: **what we have said is about the ledger of a person, not about every possible institution.**

**2.9** If a higher layer makes an item that is **unique and spendable** — something two people cannot hold at the same time — that problem returns. This is not a deficiency of Rokh; it is the nature of that item.

**2.10** And there Rokh has no way of its own and does not claim one. **Definite agreement** — each stakeholder closing the same work in their own ledger — is enough for shared, non-exclusive work, but it is **not** enough for a unique item: the same few people can accept two incompatible transfers. A unique item needs an external ritual that keeps uniqueness and order — or custody with bounded authority.

**2.11** In both cases Rokh stays what it was: **it records the grant and the receipt, and does not hold the item itself.**

**2.12** So the claim we do not make is this: "no institution ever needs consensus". What we showed is narrower and firmer — **the ledger of one person does not need it.**


## Section 3 · The axioms

**3.0** Every system stands on a few unspoken propositions. If they stay unspoken, their error spreads into the final answer without anyone knowing where it came from. These are spoken — and each of them, what it requires of **every** implementation. The code is a provisional witness, not the proof of an axiom.

> **Axiom 1** — The unit of account is the person — not the user account, not the network, not the program.
>
> **Requirement of every implementation:** one ledger, one anchor, one owner. The ledger does not accept an event whose anchor is another; it does not reject it because it is bad, it rejects it because it *does not belong here*.

> **Axiom 2** — An event, once recorded, cannot be undone.
>
> **Requirement of every implementation:** the ledger only adds. Deletion is not scissors; it is a new event that says "I no longer want that". Erasing is itself an act and is recorded.

> **Axiom 3** — Memory without attestation is a claim.
>
> **Requirement of every implementation:** what is signed, what is hashed and what is stored is one string of bytes — not three strings. If the result of reading back is another string of bytes, the event is not accepted.

> **Axiom 4** — Authority can be delegated; revoking it does not undo the past.
>
> **Requirement of every implementation:** the ruling on each event is judged only in its own causal past. What was permitted at the moment of writing is not voided by a later revocation. History has no rewinding.

> **Axiom 5** — For something to have happened, an observer is not needed.
>
> **Requirement of every implementation:** no network, no outside witness, no request to register. In the whole of Rokh there is not one code path that opens a network socket. An offline ledger is not an incomplete ledger.

> **Axiom 6** — Being correct is not a vote.
>
> **Requirement of every implementation:** no consensus, no majority, no "longer chain". Correctness is a property of the bytes, and one person alone can verify all of it.

> **Axiom 7 — negative, and just as important** — The ledger does not make meaning. Rokh says "this was written", not "this is true".
>
> **Requirement of every implementation:** no threshold turns degree into acceptance; verification and credibility are two things. And an event that has not arrived is "unknown", not "false".

**3.8** The seventh axiom is exactly where most systems slip: a ledger that starts saying "this is right" is no longer a ledger — **it is a ruler.**


## Section 4 · Mechanism

**4.1** **Bytes.** Each event has one canonical byte encoding: a given content has exactly one valid encoding. Any alternative — even one extra space — is rejected rather than accepted and repaired. This prevents two byte sequences with the same meaning from acquiring two different names.

**4.2** **Name.** The name of every event is its own hash, and **the hash is a name, not a container**: nothing comes out of it — not the text, not a pointer to the text, not a resemblance to a nearby text. Only two things can be done with it: verify equality, and point to the event. And its boundary: certain equality is proved only by comparing the bytes themselves; a collision of the full name is **improbable, not impossible** — the ledger sees it but cannot point to both unambiguously by the same name, and its ruling is still open.

**4.3** **Parent.** Every event names its parents. "Earlier" means "reachable through the parents", and no more. The clock in Rokh is an *attestation*, not an order: a number that someone stated inside an event. Two events, neither of which is reachable from the other, are **concurrent**, even if their clocks are the same.

**4.4** **Authority.** A grant has an address and its scope only narrows, never widens. A writing delegate cannot make a wider delegate; this is the only way to prevent the silent growth of authority.

**4.5** **Ceiling.** The payload of each event is small — four kilobytes in the base profile — and heavy content comes with a **descriptor**: hash, size, type. So the ledger passes through the narrowest path, and the identity of content is its hash, not its place. **This number is a choice of the design, not a limit of the world.**

**4.6** **Carrier.** The place in which the ledger stays, defined by its properties and not by its shape: self-sufficient, portable, encrypted, and with no persistent plaintext left unintended. The property is that **nothing is read without the key** — even the names of the files are hidden, so that nothing is revealed even from the listing. A folder or a partition, two items on the carrier, and the passphrase are the **base profile**, not the carrier itself.

**4.7** **Opening and recording.** Between work and ledger there is a boundary: what you see on the screen is a **working state** and is not final; an explicit act turns it into an event, and from then on it has no return. Whether the working state stays in memory or on disk has no bearing on the architecture — what matters is that **nothing passes that boundary by itself**. And recording has a point: before that point nothing, after it everything; if the power goes, the carrier returns to its last sound state.

**4.8** **Disclosure.** "Has the right to write" and "has the right to read" are two things and have two commands. If the right to write extended to the right to read, every writing delegate could show the ledger to itself. And disclosure is decided **at the moment of sealing the packet**, not at the moment of sending: what must not go never reaches the courier's hand.

**4.9** **Courier.** It is a carrier, has no key, and is not trusted. If it lies, drops, moves or repeats, **nothing in what is accepted changes** — because the judgement always rests with the receiving ledger, not with the bringer.

- **Signature** — `Ed25519`
- **Hash** — `SHA-256`
- **Payload ceiling** — 4 kilobytes
- **Carrier key** — `PBKDF2-HMAC-SHA256` · 600,000 rounds → `HKDF` → `AES-256-GCM`
- **File name** — HMAC — not a plain name
- **Door for programs** — NDJSON over a Unix socket, mode `0600`
- **Network** — none
- **Dependency** — none — only the standard library

**4.10** And this list is the **base profile**, not an axiom. None of it is our invention and none should be — hand-made cryptography is broken cryptography. But a signed byte string is never rewritten, so every change is a **new generation**: it brings its own name and its own verifier, and upgrading does not touch the past. Reading an old generation is possible as long as its verifier is kept; and if an old cipher breaks, the boundary of security changes, not the history. What we have built is a *rule*, not a *cipher*.


## Section 5 · The adversary

**5.1** A security claim that does not state the list of its adversaries is advertising. We have considered these, and for each we say what it can and cannot do.

| Adversary | What it can do | What it cannot do |
|---|---|---|
| Someone who has taken the carrier | Copy the files; see that it is a Rokh carrier. | Read anything without the key; not even see the **names of the files**. ⚠ But the size, the time of change and the pattern of growth are not hidden. |
| The courier, or any intermediate path | Drop the packet, deliver it late, repeat it, or see what is not sealed. | Make **any** change in what is accepted. Forging an event without the key is not possible, and the judgement rests with the receiver. |
| A writing delegate that has turned bad | Write whatever it wants within its own scope — until it is revoked. | Write outside the scope; widen its scope; or **draw the right of disclosure out of that same authority** — disclosure needs a separate grant, and a disclosure delegate too discloses only within the scope granted. |
| The owner of the ledger, later | Write whatever they want; and say "I no longer want that". | Quietly rewrite a past that you have seen — changing one byte means the name of that event and **the names of all its children** change. ⚠ But they can **make two branches and not show you one of them**; only an earlier witness reveals this, not the ledger itself. |
| The maker of Rokh — that is, us | Write code. That is all. | Read or send anything of your ledger: there is no network, no key with us, and the code is open. ⚠ But **open code does not prove that the body that runs is that same code**: a reproducible build gives the expected bytes, and verifying the bytes that are **installed and running** is another ring. |
| A compromised host | While it is open, see whatever is on the screen and whatever is in memory. | **Nothing.** Rokh's having no network does not help here; the boundary of trust lies before Rokh, not within it. |
| Someone who has taken the root key | **Everything.** | Nothing. We say this below, because it is the largest limitation of Rokh. |

**5.2** And one adversary for whom we do not yet have a complete answer: **a machine that has both the ledger and the key to open it, and is always on.** The way is key rotation and separating the signing key from the always-on body; its ruling is still open and we do not hide it in the document.


## Section 6 · What Rokh cannot do

**6.0** Every document that counts only what can be done lies by omission.

**6.1** **If you hand over your key, it is over.** Rokh is cryptography, not a miracle. No mechanism can keep you from handing over your key — by force or by will.

**6.2** **Revoking does not make the seen unseen.** You close disclosure; what has been seen has been seen. No system can do this, and whoever claims it lies.

**6.3** **Rokh does not say you spoke the truth; it says you said.** The ledger is an attestation of the writer and the order, not an attestation of reality. If you write a lie, you will have a signed, dated lie.

**6.4** **It does not guarantee the clock.** Rokh knows what came before what; it does not know whether it was Tuesday or Wednesday, unless someone has attested it and you trust that witness.

**6.5** **It does not keep heavy content.** It keeps the descriptor. If the file itself is lost, Rokh knows that it was and what it was — but does not bring it back.

**6.6** And the most truthful: **key recovery is not in the design** — and this is a ruling, not an incapacity. And the two losses are not the same: if the key goes, the encrypted copies are useless; if all the copies go, the key is of no use. *Ownership means responsibility*: keeping the key and multiplying the copies are both your work.


## Section 7 · Ownership

**7.1** The measure of ownership is one thing and simple: **if you cannot pick it up and go, you are not the owner.** Whatever else they say — "your data is yours", "export whenever you like" — as long as leaving is not possible, it is rent.

**7.2** So three rights come together and are not separated: **the right of data** (this is mine), **the right of privacy** (I say who sees), **the right of exit** (whenever I want I pick it up and go, and my going breaks nothing).

**7.3** The right of exit in Rokh is a property of the architecture, not a promise: the carrier is self-sufficient, without network, and works completely without us too — its being a folder or a partition is the base profile, not a condition of exit. *Leaving Rokh needs no permission, because entering did not need it either.*


> ### A token needed to write in your own ledger is rent under another name.

**7.4** For this reason Rokh has no token and will not have one. Not from piety — from consistency: a system that demands something for writing in a person's ledger gives the right of data with the right hand and takes it back with the left.

**7.5** And for this reason the code is open. But let us state its boundary too: **open code does not prove that the body that runs is that same code.** A reproducible build gives the *expected* bytes; verifying the bytes that were actually installed and run is another ring and wants its own tool. Being verifiable means that anyone can verify for themselves, and that verification must go as far as that.

**7.6** The strength of Rokh's guardian comes from its covenant, **not its claws.** Whatever authority it has was delegated to it, and every grant can be revoked.

**7.7** And the last boundary, beyond which Rokh does not go: the place of decision belongs to the human and is not counted.


## Section 8 · Work and market

**8.1** The right question is not "why would anyone buy Rokh?" — Rokh is not for sale. The right question is: **what cost does it remove that you are paying right now?**

**8.2** You have a ledger today too. But you pay rent for it, the password of its door is in someone else's hands, and if that someone closes the door tomorrow or changes their terms, your year of work is left inside. This cost is paid; it is just not written on the invoice.

**8.3** And the value of Rokh is not in writing — writing is cheap. It is in **showing without an intermediary**: that you can show something without a platform's permission and the other side can verify it themselves. But exactly this much and no more: Rokh proves **the writer, the byte, the lineage and the authority presented** — not the external truth of what you have said, nor the completeness of what you have shown.

**8.4** Three probable forms — *a conjecture, not a promise*: a free professional who carries their own record of work and is pledged to no platform · someone who shows their professional or medical history directly and in measure, neither less nor more · and a software agent every act of which has a receipt, and whose owner knows on whose authority it acted and where to go back from.

**8.5** All three of these may be wrong. We do not know the field; we only build the language and the body.


> ### A language game is either nonsense or useful — and the field makes this known, not the author.

**8.6** So Rokh does not prescribe its form of use. It gives a grammar — eight verbs, a few rules, one simple door — and lets **the right form of use be found in the free market**. A system that says beforehand how people must use it has either guessed its consumer wrongly, or does not want to understand.

**8.7** And the measure of failure is clear too, so that it can be checked later: if no one but us writes in it, this language game was nonsense. **A ledger that no one writes in is not a ledger; it is a shelf.**


## Section 9 · Conclusion

**9.1** We proposed a ledger that belongs to the person and asks no one's permission to be correct. We did not solve the problem of double spending; we showed that it does not arise in a person's ledger — and what was built to solve it steps aside together with that problem.

**9.2** Verification took the place of the vote and the byte took the place of trust. And nothing took the place of the ruling — that place was left empty, deliberately, for the human.

**9.3** Rokh is upstream, but not upstream of everything: **upstream of meaning and lineage**, not upstream of cipher and network. It takes key, route and distribution into service from the existing free wheels and does not build them anew. What it has of its own is one thing, and that one thing is not anywhere else: *that a person, in a lineage, performed which addressed verb, and on whose authority.*

**9.4** And the place of each wheel is clear, because the map of the realms already exists. Rokh itself is none of these realms: **its body runs in one of them, and what is seen in all eight is the trace of meaning and lineage** — that every work can say what happened and on whose authority. Judging them is not Rokh's.

| Realm | What is there |
|---|---|
| Body | Linux, hardware, board and sensor |
| Environment | Cluster and container, deployment helm, observatory, code history, Holochain — **and the body of Rokh** |
| Ledger | The page of reading and writing, the single-file database, the ledgers of life |
| Agreement | The mesh route, key and identifier systems, tunnel and access keys, covenants |
| Instruction | The grammar that emerges from the individual's own ledger |
| Value | Value, property, inheritance |
| Institution | Allegiances and institutions — **the institution sits here** |
| Work | Local programs, orchestrator, panels |

**9.5** We describe the state along three axes, rather than as a list of absences. **Settled in the design, not yet implemented:** sealing for a specified recipient, opening as an actual folder, mutual acceptance and joint work across ledgers, and the application covenant, whose design is settled but whose runtime integration has not been implemented. **Open design questions:** key rotation, mirroring between two owners, the bond reader, succession, the byte format for selective disclosure, and a full-name collision, should one ever be found. **Outside the design:** key recovery. And **the field test**, which belongs to none of these three: whether anyone besides us writes in this ledger.
