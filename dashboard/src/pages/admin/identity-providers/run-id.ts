/**
 * `?test=<id>` is 43 base64url characters; anything else is ignored. Its own
 * module, so the route's search schema reads it without loading the page.
 */
export const runIdPattern = /^[A-Za-z0-9_-]{43}$/;
