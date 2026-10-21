## Why

<!--
Explain the problem and why this change is needed. Link the relevant issue.
Use "Closes #N" only when this PR fully resolves it; otherwise use
"Follow-up to #N" or a plain reference.
-->

## What changed

<!--
Describe the concrete behavior and important design decisions. Include real
command/output or JSON examples for user-visible and structured-output changes.
-->

## Compatibility

<!--
State the effect on existing CLI syntax, output, exit codes, 188-byte TS and
192-byte M2TS support, schemas, and resource/output-size characteristics as
applicable. Say explicitly when there is no compatibility impact.
-->

## Documentation

<!--
For changes to CLI syntax, flags/defaults, help text, text or JSON output, or
exit codes, describe the documentation audit and updates. Check README.md,
OPTIONS.md, CLI help/examples, JSON schema, and the README demo as applicable.
Explain why any expected document or generated artifact does not need a change.
-->

## Validation

<!--
List the exact commands run and their observed results. Analysis changes must
include an independent TSDuck, gots, astits, or ffprobe comparison and explain
any discrepancy. Do not list checks that were not run.
-->

<!--
Check each item after completing it or confirming that it is not applicable.
Explain non-obvious N/A decisions in the sections above.
-->

- [ ] The final pushed commit passed `.githooks/pre-push`.
- [ ] The documentation-policy check passes and advisory warnings were assessed.
- [ ] Public-interface changes are reflected in README, OPTIONS, help/examples,
      schemas, and the README demo where applicable.
- [ ] Generated assets are current and were inspected where applicable.
- [ ] Independent reference-tool validation is included where applicable.

