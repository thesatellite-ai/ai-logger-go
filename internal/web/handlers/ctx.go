package handlers

import "context"

// ctxLike is a tiny indirection so adjacentInSession (and any future
// helper) can accept any context-like value without forcing a direct
// "context".Context import in every signature. Keeps the signatures
// short while staying type-safe.
type ctxLike = context.Context

func asContext(c ctxLike) context.Context { return c }
