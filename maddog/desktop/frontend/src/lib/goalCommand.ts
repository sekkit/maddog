const GOAL_FLAGS = new Set([
  "--strict",
  "--research",
  "--auto-research",
  "--deep",
  "--simple",
  "--no-research",
]);

export interface GoalCommandDisplay {
  objective: string;
  hasFlags: boolean;
}

// The controller owns the command semantics. This parser only keeps the
// optimistic Desktop profile aligned with the objective the controller sees.
export function goalCommandDisplay(arg: string): GoalCommandDisplay {
  const parts = arg.trim().split(/\s+/).filter(Boolean);
  let flags = 0;
  while (flags < parts.length && GOAL_FLAGS.has(parts[flags].toLowerCase())) {
    flags += 1;
  }
  return {
    objective: parts.slice(flags).join(" "),
    hasFlags: flags > 0,
  };
}
