# Product Sense: AI-Native Memos

## Product Promise

Memos remains a fast, self-hosted place to capture thoughts. AI should reduce the effort required to find, understand, and organize those thoughts without turning the product into a heavyweight document suite or surrendering user control.

## Target Users

- Individuals running a private personal knowledge base.
- Small trusted groups sharing a self-hosted instance.
- Users who want their own model provider credentials and deployment control.
- External-Agent users who continue to use the existing MCP endpoint independently.

## Product Principles

1. **Capture stays immediate.** AI configuration or indexing never blocks writing a Memo.
2. **Useful without extra infrastructure.** The application and current database provide the complete baseline experience.
3. **Search before automation.** Reliable retrieval and citations precede autonomous workflows.
4. **Read automatically, write deliberately.** Authorized reading/search may run automatically; creation and update require confirmation.
5. **Preview the result, not implementation mechanics.** Proposal review looks like a Memo preview, not a Git diff.
6. **Graceful degradation.** Keyword search remains available without embeddings or during reindexing.
7. **Evidence over confident prose.** Answers about the note collection cite the Memos that support them.
8. **Visible data movement.** Users can tell when eligible note content is processed by an externally configured provider.

## Core Experiences

### Ask

The user asks a question, receives a streamed answer, and can open every cited Memo. Follow-up questions remain in a private conversation.

### Find

One search field handles partial keywords, spelling variations, tags, titles, body text, and semantic intent. Results explain why they matched.

### Organize

The Agent can suggest a new Memo or updated Memo. The user sees the finished result and a short change summary, then applies, edits, or discards it.

### Capture from Chat

A useful Chat result can become a Memo only through a create proposal. Chat history itself never appears in the timeline.

## Success Measures

- Search success: users open or cite a result without immediately reformulating the query.
- Grounding: answers about Memos include valid, authorized citations.
- Proposal acceptance: accepted proposals require little or no manual repair.
- Safety: zero unauthorized retrievals and zero writes without confirmation.
- Resilience: Memo capture and keyword search remain operational during provider or embedding failure.
- Simplicity: a default deployment requires no service beyond Memos and its configured database.
- Boundedness: retrieval remains within documented resource budgets and reports partial/degraded coverage instead of hiding it.

Metrics must not require transmitting private content to a Memos-operated telemetry service. Self-hosted operators may inspect local aggregate diagnostics.
