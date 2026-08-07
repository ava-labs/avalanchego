# Before / After Examples

## Part 1 — Official STE Examples

These illustrate real ASD-STE100 rules, drawn from public secondary sources (see `references/writing-rules.md`). They are paraphrased illustrations of the rule, not quotes from the standard itself.

| Rule | Before | After | Why |
|---|---|---|---|
| One meaning per word | "Verify the system." / "Check the connections." / "Confirm receipt." | "Make sure the system is correct." (one approved term used consistently) | Three near-synonyms force the reader to guess whether they mean the same action. |
| One part of speech per word | "Oil the valve." | "Apply oil to the valve." | If "oil" is approved only as a noun, using it as a verb breaks the one-word-one-role guarantee. |
| Precise verb meaning | "Follow the safety instructions." | "Obey the safety instructions." | "Follow" can mean "come after" or "obey" — STE picks the unambiguous one. |
| Simple tense only | "We have received the technical reports from HQ." | "We received the technical reports from HQ." | Present perfect adds a second parse ("received, and still relevant now?") that simple past avoids. |

## Part 2 — Applied to Agent Output

These are original examples built for this skill's actual use case: rewriting AI agent output so another agent, a translation layer, or a non-native reader can parse it without ambiguity. They are illustrations, not quotes from any real system.

### Example A — Tool description

**Before:**
> This tool will attempt to synchronize state across the various backends that have been configured, and if a conflict is detected it may resolve it automatically depending on the strategy that has been set, or otherwise it will surface the conflict for manual review.

**Violations flagged:**
- Two instructions in one sentence (sync + resolve/surface).
- Present-perfect and modal stacking ("have been configured", "may resolve", "has been set") — multiple hedges compound ambiguity.
- 55 words, far over the 25-word descriptive cap.

**After:**
> The tool synchronizes state across the configured backends. If it finds a conflict, it checks the current strategy. If the strategy allows automatic resolution, the tool resolves the conflict. If not, the tool reports the conflict for manual review.

### Example B — Error message

**Before:**
> An error may have occurred while processing your request due to a possible mismatch in the expected data format, which could be caused by an outdated client version.

**Violations flagged:**
- Passive voice with unclear actor ("an error may have occurred").
- Present perfect + double hedge ("may have occurred", "could be caused").
- One sentence carrying two separate claims (error occurred; possible cause).

**After:**
> The request failed. The data format did not match what the server expected. Check your client version — an outdated client is the most common cause.

### Example C — Inter-agent instruction

**Before:**
> Once the upstream job has completed and assuming no errors were raised, the downstream agent should proceed to consume the output artifact, though it is worth noting that partial artifacts are sometimes produced under timeout conditions.

**Violations flagged:**
- Present perfect ("has completed") and subordinate-clause stacking ("assuming...", "though it is worth noting...").
- One sentence, three separate facts (completion condition, next action, edge-case warning).
- 42 words, over the 20-word instruction cap.

**After:**
> Wait for the upstream job to finish with no errors. Then read the output artifact. Warning: a timeout can produce a partial artifact. Check the artifact is complete before you use it.

## How to Read These Examples

Part 1 shows the actual rules this skill is built on. Part 2 shows the transfer: the same discipline — one meaning per word, active voice, simple tense, one instruction per sentence, explicit conditions instead of buried subordinate clauses — makes machine-to-machine and cross-language text safer to parse, not just aircraft manuals.
