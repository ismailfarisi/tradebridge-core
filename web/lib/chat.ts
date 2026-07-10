import type { ChatMessage } from "./api";

export type ChatState = {
  messages: ChatMessage[];
  pending: boolean;
  error: string | null;
  rfqId: string | null;
};

export type ChatAction =
  | { type: "send"; content: string }
  | { type: "reply"; assistant: string; rfqId?: string }
  | { type: "error"; error: string }
  | { type: "reset" };

export const initialChatState: ChatState = {
  messages: [],
  pending: false,
  error: null,
  rfqId: null,
};

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "send":
      return {
        ...state,
        messages: [...state.messages, { role: "user", content: action.content }],
        pending: true,
        error: null,
      };
    case "reply":
      return {
        ...state,
        messages: [...state.messages, { role: "assistant", content: action.assistant }],
        pending: false,
        error: null,
        rfqId: action.rfqId ?? state.rfqId,
      };
    case "error":
      return { ...state, pending: false, error: action.error };
    case "reset":
      return initialChatState;
    default:
      return state;
  }
}
