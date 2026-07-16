# Sessions use one writer lease and transcript CAS

Every writable canonical transcript path has one cross-process Session Runtime
Lease, while every save also compares a monotonic revision and content digest
against its Transcript CAS Baseline. A stale or divergent writer never
overwrites durable history; it preserves work in a bounded Recovery Branch.
Runtime replacement acquires or transfers authority before publication, and
tab-scoped asynchronous results must still match the current runtime generation.
