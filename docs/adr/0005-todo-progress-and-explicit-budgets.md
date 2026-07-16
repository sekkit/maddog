# Todo progress is semantic and resource budgets are explicit

Interactive executor and planner sessions use host-observed semantic progress,
not hidden persistent step limits: one current todo is allowed, repeated
receipts do not renew progress, a stalled run is nudged after eight tool rounds
and paused after sixteen. Explicit caller budgets remain available for CLI
one-shots, bots, evaluation, guardian, advisor, child agents, SkillOpt, and
durable goals; these are resource contracts rather than invisible defaults.
