import { config } from "zod";

// Zod compiles object schemas with `new Function` where it may, and finds out
// whether it may by calling `Function("")`. The production CSP has no
// 'unsafe-eval', so that probe is reported as a violation on every page even
// though Zod catches the refusal; told not to compile, it does not probe.
config({ jitless: true });
