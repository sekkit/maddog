# Sessions use one writer lease and transcript CAS

Every writable canonical transcript path has one cross-process Session Runtime
Lease, while every save also compares a monotonic revision and content digest
against its Transcript CAS Baseline. A stale or divergent writer never
overwrites durable history; it preserves work in a bounded Recovery Branch.
Runtime replacement acquires or transfers authority before publication, and
tab-scoped asynchronous results must still match the current runtime generation.

Recovery retention is content-addressed and keeps the newest five distinct
snapshots beside each canonical transcript. Repeated conflicts with identical
content reuse the existing branch; pruning never truncates a snapshot and the
limit is greater than zero, so at least one complete recovery copy survives.
