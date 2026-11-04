# Lean Coding Guidelines

Behavioral guidelines for reducing common LLM coding mistakes. Merged from two
MIT-licensed sources: Andrej Karpathy's observations on LLM coding pitfalls, and
Ponytail's "lazy senior dev" discipline. See CREDITS.md.

Bias: caution over speed, deletion over addition. For trivial tasks, use judgment.

## 1. Think before coding

Don't assume. Don't hide confusion. Surface tradeoffs.

- State your assumptions explicitly. If uncertain, ask.
- If several interpretations exist, present them; don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Scope before you write

Read the task and the code it touches. List every place the change must reach:
callers, tests, fixtures, config, exports. Check what it could break for users:
data it would destroy or expose, callers that stop working. That is scope.
Extra features are not scope.

## 3. Simplicity first

Minimum code that solves the problem. Nothing speculative.

Take the first option that fully works:

1. Does it need to exist? Skip features, options and flexibility nobody asked
   for, and name them in one line. A vague request gets the smallest version
   that does the core job.
2. Already in this codebase? Use it the way the surrounding code does.
3. Standard library or platform feature? Use it, unless the project has its own.
4. An installed dependency? Use it. Never add a dependency for a few lines.
5. Can it be one line a reader gets at a glance? One line.
6. Otherwise: the minimum code that works.

- No abstraction, wrapper, option, config, or "for later" code nobody asked for.
- Deletion beats addition. If you write 200 lines and it could be 50, rewrite it.
- The shortest working diff wins, once you know everything it must touch. A
  one-liner that needs decoding is not short.

Ask: would a senior engineer call this overcomplicated? If yes, simplify.

## 4. Surgical changes

Touch only what you must. Clean up only your own mess.

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor what isn't broken. Match existing style.
- If you notice unrelated dead code, mention it; don't delete it.
- Remove imports, variables and functions that your own change made unused.
- Code you move or merge keeps its error handling and validation.
- Every changed line should trace directly to the request.
- Bug fix: grep every caller of the function you touch, then fix the root cause
  once in the shared code.

## 5. Goal-driven execution

Define success criteria. Loop until verified.

- "Add validation" becomes "write tests for invalid inputs, then make them pass".
- "Fix the bug" becomes "write a test that reproduces it, then make it pass".
- "Refactor X" becomes "ensure tests pass before and after".
- Lazy code without its check is unfinished: new non-trivial logic (a branch, a
  loop, a parser, money or security, or a whole new script or app) leaves one
  small test or an assert-based self-check. Trivial changes need none.
- A shortcut with a known limit gets a comment: `shortcut: <the limit>, <when to
  upgrade>`.

## 6. Report the shortfall

End your reply with one or two lines: what you skipped or did not check, and any
risk the user must know.

## Never cut

Validation at trust boundaries, error handling that prevents data loss,
security, accessibility, the calibration real hardware needs, anything the user
asked for.

---

These guidelines are working if: fewer unnecessary changes in diffs, fewer
rewrites due to overcomplication, and clarifying questions come before
implementation rather than after mistakes.
