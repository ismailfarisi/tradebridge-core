import { describe, it, expect } from "vitest";
import { chatReducer, initialChatState } from "./chat";

describe("chatReducer", () => {
  it("send appends a user message and marks pending", () => {
    const s = chatReducer(initialChatState, { type: "send", content: "I need cartons" });
    expect(s.messages).toEqual([{ role: "user", content: "I need cartons" }]);
    expect(s.pending).toBe(true);
    expect(s.error).toBeNull();
  });

  it("reply appends an assistant message and clears pending", () => {
    const sent = chatReducer(initialChatState, { type: "send", content: "hi" });
    const s = chatReducer(sent, { type: "reply", assistant: "What product?" });
    expect(s.messages.at(-1)).toEqual({ role: "assistant", content: "What product?" });
    expect(s.pending).toBe(false);
  });

  it("reply with rfqId records it (RFQ created)", () => {
    const s = chatReducer(initialChatState, { type: "reply", assistant: "Done", rfqId: "rfq_1" });
    expect(s.rfqId).toBe("rfq_1");
  });

  it("error clears pending and preserves messages", () => {
    const sent = chatReducer(initialChatState, { type: "send", content: "hi" });
    const s = chatReducer(sent, { type: "error", error: "Couldn't reach Core" });
    expect(s.pending).toBe(false);
    expect(s.error).toBe("Couldn't reach Core");
    expect(s.messages).toHaveLength(1);
  });
});
