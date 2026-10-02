# Authority, and the monotonicity theorem

> This document closes the question the earlier implementation left open:
> what revocation means on a concurrent branch.

## 1. The old knot

The earlier implementation judged a right like this:

```
hasRight(k, module, action, D) := not revoked(k, D) and exists e in D: grant(e, k, ...)
```

`revoked(k, D)` searched the **whole ledger**. The consequence:

> a late-arriving revocation could **shrink** the accepted set.

Adding an event killed another, already accepted, event. Acceptance was **not
monotonic**. The earlier design document called this a known break, and its
proposed cure - "revocations get delivery priority" - is a rule about
*delivery*, not a fix for *validity*. However fast you deliver, a partitioned
node still learns late and the problem stands.

## 2. The new rule

```
causalPast(e) := transitive closure of e's parents

authorized(e) :=
    e is genesis and its id is this ledger's anchor
  or exists d in causalPast(e):
        verb(d) = rokh.grant
    and d = authority(e)
    and subject(d) = author(e)
    and scope(d) covers address(e)        [for non-reserved verbs]
    and not exists r in causalPast(e): verb(r) = rokh.revoke and target(r) = d
    and authorized(d)
```

The difference is one phrase: **e's causal past**, not the whole ledger.

## 3. The theorem

> `causalPast(e)` is fixed the moment `e` is signed and never changes.

Proof in one line: e's parents are inside e's signed bytes and are addressed by
hash, so no new event can become an ancestor of e. The ancestor set is
**immutable**.

Consequences:

1. **The verdict is final.** `authorized(e)` is a pure function of e and its
   ancestors. No arriving event changes it.
2. **Acceptance is monotonic.** Adding can only turn "pending" into "accepted"
   or "rejected", never "accepted" into "rejected".
3. **Revocation means what it should:** from here on, with no bearing on the
   past.

Three states, each with an exact meaning:

| verdict | meaning |
|---|---|
| **accepted** | ancestry complete and everything checks. Permanently. |
| **rejected** | ancestry complete and something does not check. Permanently. |
| **pending** | ancestry has not arrived. **Not arrived is not the same as not existing.** |

## 4. The price we pay, and why it is right

A node that has not got the revocation in its causal past keeps producing
**valid** events on its own branch — a partitioned node that never saw it, and
equally a node that saw it and built on an older parent anyway.

That is not a defect; it is the exact report the ledger can give. Each such
event was built on a branch whose causal past does not contain the
revocation. **The ledger does not know what the writer knew:** the absence of a
revocation in an event's lineage is not evidence that the writer had not seen
it, and a verdict is never a finding about knowledge or intent. What the
ledger promises is narrower and firm — a door that holds the revocation writes
no further event of that delegate on top of it, and from every merge that
reaches it the grant is dead. At a merge, the revocation is in the merge
point's causal past, so **from there on** the grant is dead. Contradictory
accounts sit side by side; the core does not flatten them into a manufactured
truth, and it does not pretend to read minds.

Likewise a signature proves who signed which bytes — nothing more. It is not
proof that the signer consented to what the bytes say, nor that what they say
is true of the world; those are questions for people, outside the ledger
(§12.5 of the treatise).

```
        grant
          |
         e1 ---- revoke ---- e2     rejected (the revocation is in its past)
          |          |
          \---- e3   |               accepted (this branch had not seen it)
                 |   |
                 \-merge---- e4     rejected (now it is in the past)
```

All six verdicts are exercised in `TestRevocationIsCausalNotGlobal` and again
end to end through the CLI; the knowing fork — a delegate that has the
revocation in hand and writes on an older parent — is witnessed in
`proof/TestRevocationClosesTheFutureAndDoesNotUnsee`.

## 5. Implementation

Every accepted event carries two **monotone, only-growing** sets:

```
grants(e)  = union of grants(parents)  + {e if it is a grant}
revoked(e) = union of revoked(parents) + {target if it is a revocation}
```

Neither ever shrinks, so union at a merge point is trivial and a revoked grant
can never come back to life.

`authorized(e)` becomes two binary searches: `authority(e) in grants(parents)`
and `authority(e) not in revoked(parents)`.

**Memory cost:** these sets are **interned**. Most events hold exactly their
parent's set, so one copy is shared across thousands of events. Size grows with
the number of **distinct authority states**, not the number of events, and
since grants and revocations are few it stays small.

## 6. The smaller rules, and their reasons

| rule | why |
|---|---|
| Root omits the `authority` field entirely | so there is exactly one form |
| A sub-delegation is never wider than the delegator's right | `MachineScope ⊆ PropertyScope`, in code rather than in prose |
| Revocation is root's right or the **granter's own** | nobody withdraws somebody else's grant, so "revoke" never needs to be in a verb list |
| A revoke target must be in the causal past | you cannot withdraw what you have not seen |
| A rejected parent makes a rejected child | the chain stops at the break |
| Order: topological, ties broken on **id bytes** | no clock, no counter, no arrival order |
| The ledger's only input is **bytes** | every input goes through the signature door; no caller can hand it a hand-built event |

## 7. Still open

- **Root recovery: deliberately absent.** A lost secret is not reconstructed;
  a mirror preserves events, it does not recover a secret.
- **The social meaning of a divergent branch.** The core keeps both accounts.
  What an interpreter should do with them - show which, warn about which - is
  not the core's business and has not been designed.
- **Bundle and transport frame limits.** The carry layer. Not ruled on.
