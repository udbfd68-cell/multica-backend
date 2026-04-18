"""Billing & cost calculation for the Aurion Agent Runtime.

Computes per-session and aggregate costs from token usage + model pricing.
Pricing table is defined in models.py (MODEL_PRICING).
"""

from __future__ import annotations

from packages.core.models import (
    MODEL_PRICING,
    SessionCost,
    UsageSummary,
)


def compute_session_cost(
    session_id: str,
    model_id: str,
    usage: dict,
) -> SessionCost:
    """Compute USD cost for a single session's usage."""
    pricing = _get_pricing(model_id)

    input_tokens = usage.get("input_tokens", 0)
    output_tokens = usage.get("output_tokens", 0)
    cache_read = usage.get("cache_read_input_tokens", 0)

    input_cost = (input_tokens / 1_000_000) * pricing["input"]
    output_cost = (output_tokens / 1_000_000) * pricing["output"]
    cache_cost = (cache_read / 1_000_000) * pricing["cache_read"]

    return SessionCost(
        session_id=session_id,
        model_id=model_id,
        input_tokens=input_tokens,
        output_tokens=output_tokens,
        cache_read_input_tokens=cache_read,
        input_cost_usd=round(input_cost, 6),
        output_cost_usd=round(output_cost, 6),
        cache_read_cost_usd=round(cache_cost, 6),
        total_cost_usd=round(input_cost + output_cost + cache_cost, 6),
    )


def compute_usage_summary(
    sessions_data: list[dict],
) -> UsageSummary:
    """Compute aggregate usage summary across multiple sessions."""
    total_input = 0
    total_output = 0
    total_cache = 0
    total_cost = 0.0
    by_model: dict[str, dict[str, float]] = {}

    for s in sessions_data:
        usage = s.get("usage", {})
        model_id = s.get("model_id", "unknown")

        cost = compute_session_cost(
            session_id=s.get("session_id", ""),
            model_id=model_id,
            usage=usage,
        )

        total_input += cost.input_tokens
        total_output += cost.output_tokens
        total_cache += cost.cache_read_input_tokens
        total_cost += cost.total_cost_usd

        if model_id not in by_model:
            by_model[model_id] = {
                "input_tokens": 0,
                "output_tokens": 0,
                "cache_read_input_tokens": 0,
                "total_cost_usd": 0.0,
                "session_count": 0,
            }
        by_model[model_id]["input_tokens"] += cost.input_tokens
        by_model[model_id]["output_tokens"] += cost.output_tokens
        by_model[model_id]["cache_read_input_tokens"] += cost.cache_read_input_tokens
        by_model[model_id]["total_cost_usd"] = round(
            by_model[model_id]["total_cost_usd"] + cost.total_cost_usd, 6
        )
        by_model[model_id]["session_count"] += 1

    return UsageSummary(
        total_sessions=len(sessions_data),
        total_input_tokens=total_input,
        total_output_tokens=total_output,
        total_cache_read_input_tokens=total_cache,
        total_cost_usd=round(total_cost, 6),
        by_model=by_model,
    )


def _get_pricing(model_id: str) -> dict[str, float]:
    """Look up pricing for a model, with fallback for unknown models."""
    # Exact match
    if model_id in MODEL_PRICING:
        return MODEL_PRICING[model_id]

    # Prefix match (e.g. "ollama/llama3" matches "ollama/*")
    for pattern, pricing in MODEL_PRICING.items():
        if pattern.endswith("/*"):
            prefix = pattern[:-2]
            if model_id.startswith(prefix):
                return pricing

    # Fallback: use Claude Sonnet pricing as default
    return MODEL_PRICING.get(
        "claude-sonnet-4-20250514",
        {"input": 3.0, "output": 15.0, "cache_read": 0.3},
    )
