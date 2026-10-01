## What changed

## Why

## How it was checked

- [ ] Every statement says whether it was measured, read in the code, extrapolated or proposed.
- [ ] Every measurement names its command, its host and the commit it ran against, and its raw output is kept beside it with its SHA-256.
- [ ] Nothing reads as a ruling; what needs one is a question.
- [ ] Nothing describes a weakness in security that is not yet repaired.
- [ ] `cd tools/records && go run . -root ../.. check` passes.
