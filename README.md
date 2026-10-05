<h1 align="center">Rokh</h1>

<p align="center"><strong>Let the machine work. Keep authority with the person.</strong></p>

<p align="center">
  <a href="docs/رساله.md">Treatise</a>&nbsp;&nbsp;·&nbsp;&nbsp;
  <a href="docs/without-consensus.md">The ledger without consensus</a>&nbsp;&nbsp;·&nbsp;&nbsp;
  <a href="source/README.md">Source</a>&nbsp;&nbsp;·&nbsp;&nbsp;
  <a href="lab/README.md">Lab</a>&nbsp;&nbsp;·&nbsp;&nbsp;
  <a href="work/README.md">Work</a>
</p>

<br>

Every person today has a ledger that is not their own. What you do, say, and
make is recorded in accounts you cannot carry, and the service behind them can
rewrite it, shut you out, or read it without asking. When that service changes
or closes, your history breaks or is held hostage.

Rokh turns this around. First comes the person, with their key; then their own
ledger, which they can pick up and take wherever they go; then the narrow
rights they lend to devices and programs, and can take back; and last, whatever
is built on top: a blog, a shop, a wallet, an agreement. Programs come and go;
the record and the owner's right outlive them.

This is not only about keeping things hidden. It is about ownership: the person
decides who sees, and how what is theirs is kept, carried, and exchanged.

Rokh begins with a person living and working in the world: meeting others,
answering what happens, and acting in ways that reach other lives. The body
marks a concrete boundary around their life and time, the fruits of their work,
and what belongs to them. Not everything a person meets or does becomes an
event. Recording is an explicit act, and you make it in a sentence, with Rokh's
eight verbs:

<p align="center">
  <kbd>read</kbd>&nbsp;<kbd>write</kbd>&nbsp;<kbd>bring</kbd>&nbsp;<kbd>see</kbd>&nbsp;<kbd>entrust</kbd>&nbsp;<kbd>open</kbd>&nbsp;<kbd>leave</kbd>&nbsp;<kbd>carry</kbd>
</p>

```
write at home/journal: I walked to the river.
```

Rokh shows you what this would record, and records nothing yet.

```
write
```

Now it is an event in your ledger, signed with your key and placed after what
came before it. The first sentence prepared the writing; the second recorded
it. Rokh records that the sentence was written, not that the walk happened. An
accepted event is never rewritten; a correction is another event.

Time in Rokh is not a clock; it is lineage. Every event sits at an address,
like `home/journal`, and names the events before it, so your ledger is a graph
of addressable events that rolls forward without lockstep. Its copies can live
apart, at home and at work, grow on their own, and come back together without
either erasing the other. Another person's ledger stays another person's, even
when you work together.

Rights in Rokh are asymmetric. Keeping a record is not sharing it, writing is
not reading, and acting for someone is not acting without limit. You can let a
program write in one corner of your ledger without letting it read or disclose
what is there, and a delegate cannot pass on more than they received. When you
take that right back, word of it travels with your ledger: each copy it reaches
refuses that program's writing from then on, while whatever was written before,
anywhere, stays as it was, and what was seen stays seen.

Rokh is whole without a network. Its records can travel on a drive, or with a
courier across a network or a mesh of your own machines, rolling from copy to
copy in stages. The route carries them; it does not decide their authority. The
courier holds no key, and if it drops, delays, or repeats something, nothing
accepted changes, because each ledger checks what arrives for itself. With one
always-on machine in between, your other machines never need to be on at the
same time.

Checking needs no server, no token, and no majority: you check the copy you
hold, alone and offline, though no check can prove you were shown every branch.
Mathematics does not vote for the majority, and a model gets no vote either. An
AI can help you read and search your ledger, but what is accepted is settled by
the plain check of bytes, signatures, and authority, and no confidence score
turns into "accepted".

Rokh is also meant to live inside robots and AI agents, as their long-term
memory, and we hope the tools that build them will carry it and run it
axiomatically. As machines begin to act in the world, the edges of authority
matter more. When a machine works for someone, we should be able to ask which
key acted, who authorized the step, what it intended, what it reported
afterward, and whom it affected. A program built on Rokh should record its
intent before each step and its result after, failure included; a step whose
end is lost closes as unknown, never as success. Owning a machine gives a
person authority to assign it work within their own boundary, never authority
over the people it reaches. Rokh does not decide who answers for harm; it keeps
the record that lets people ask.

The same ground can carry exchange without turning anyone's ledger into a
currency. Rokh has no coin of its own, and writing in it needs none. A shop or
a private issuer writes a note: a few hundred signed bytes that can sit on a
tag or inside a physical coin and pass from hand to hand with no network at
all. Whoever takes it records in their own ledger who handed it over. Its worth
rests on what its issuer promises and on the people it passed through, and the
issuer may also bank it online, as a service. Signatures alone cannot stop a
note from being spent twice or prove that what was promised exists; that takes
the issuer's own rule, and the issuer answers for it. Money here is not money
by consensus. It is a good that someone makes and offers.

Rokh is not a judge, but when its owner chooses, it can make its graph of
events available to a verifier. It records what was done, by which key, on what
authority, after what, and whoever is shown that graph can check it for
themselves. Whether it was true, or wise, is not its question; that decision
stays with the person.

If you cannot pick it up and go, you are not the owner. Leaving Rokh needs no
permission, because entering did not need it either.

Rokh is written twice, as a [treatise](docs/رساله.md) in Persian and as
[code](source/README.md), and the two have grown as one piece of work. A
sentence gives a rule its use; an axiom sets its boundary; a data structure and
a running program test what follows. Code can uncover an ambiguity in the
words, and the words can expose a fault in the code. Run it, test its rules
against real situations, and bring counterexamples: an issue may question a
rule as well as report a bug, and a pull request may propose a clearer sentence
as well as a feature.

The test ahead is simple: whether anyone but its authors comes to write in it.

<p align="center"><em>A ledger that no one writes in is not a ledger; it is a shelf.</em></p>

<br>

## [Read the treatise →](docs/رساله.md)

**[رساله](docs/رساله.md)**, the treatise: the founding text of Rokh, in
Persian, and the original. With the ledger without consensus it specifies
Rokh. Beside it in `docs/` are [the ledger without
consensus](docs/without-consensus.md), an English rendering of its Persian
original, and [the contract of version 1](docs/contracts/v1.md).

## The four folders

Each folder holds one standing of what is said about Rokh, so that nobody has
to ask whether a sentence is a ruling, a finding or a wish.

| folder | standing | holds |
|---|---|---|
| [`source/`](source/README.md) | what runs | the code, its tests, its own documents, and [STATE.md](source/STATE.md): what is met, what is not, what was never run |
| [`docs/`](docs/README.md) | what is ruled | the treatise, the ledger without consensus, the contract, and the register of rulings |
| [`lab/`](lab/README.md) | examined, not ruled | studies with their evidence and raw data, and the questions that wait for the owner's ruling |
| [`work/`](work/README.md) | to be examined or done | missions anyone, a person or a program, can pick up, each with the test that says it is done |

A thing moves forward only by the act that defines its next standing: work is
examined into the lab, the lab's questions are ruled into the docs, and the
docs are built into the source; where the source and the docs differ, the
difference is work. Only the owner rules and merges.

## Where to begin

- **To run it:** [`source/`](source/README.md): build, check, and the screen
  without a ledger.
- **To help:** a mission of [`work/`](work/README.md) whose status is `ready`.
- **To ask the owner:** a question of [`lab/`](lab/README.md), with the
  question form.
- **To propose a change:** [CONTRIBUTING.md](.github/CONTRIBUTING.md). Anyone
  who changes this repository, person or program, reads
  [AGENTS.md](AGENTS.md) first.
- **A weakness in security** is reported privately, as [the security
  policy](.github/SECURITY.md) says; never in an issue.

## License

Copyright (c) 2026 The Karvareh authors. Rokh is free software under the GNU
Lesser General Public License, version 3 or later: [LICENSE](LICENSE), with
the GNU General Public License it rests on in [GPL-3.0.txt](GPL-3.0.txt).
Whether the texts, the studies and the missions stay under it is question
[D-23](lab/questions/D-23-license-of-texts.md), open until the owner rules.
