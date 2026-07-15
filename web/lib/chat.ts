import type { AttachmentMeta, ChatMessage } from "./api";

export type ChatState = {
  messages: ChatMessage[];
  pending: boolean;
  streaming: boolean; // last message is a partially streamed assistant bubble
  error: string | null;
  rfqId: string | null;
};

export type ChatAction =
  | { type: "send"; content: string; attachmentIds?: string[]; attachments?: AttachmentMeta[] }
  | { type: "delta"; text: string }
  | { type: "reply"; assistant: string; rfqId?: string }
  | { type: "error"; error: string }
  | { type: "reset" };

export const initialChatState: ChatState = {
  messages: [],
  pending: false,
  streaming: false,
  error: null,
  rfqId: null,
};

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "send":
      return {
        ...state,
        messages: [
          ...state.messages,
          {
            role: "user",
            content: action.content,
            ...(action.attachmentIds?.length
              ? { attachment_ids: action.attachmentIds, attachments: action.attachments }
              : {}),
          },
        ],
        pending: true,
        streaming: false,
        error: null,
      };
    case "delta": {
      const messages = state.streaming
        ? [
            ...state.messages.slice(0, -1),
            {
              role: "assistant" as const,
              content: state.messages[state.messages.length - 1].content + action.text,
            },
          ]
        : [...state.messages, { role: "assistant" as const, content: action.text }];
      return { ...state, messages, streaming: true };
    }
    case "reply": {
      // The done payload is authoritative: replace the streamed bubble.
      const messages = state.streaming
        ? [...state.messages.slice(0, -1), { role: "assistant" as const, content: action.assistant }]
        : [...state.messages, { role: "assistant" as const, content: action.assistant }];
      return {
        ...state,
        messages,
        pending: false,
        streaming: false,
        error: null,
        rfqId: action.rfqId ?? state.rfqId,
      };
    }
    case "error":
      return { ...state, pending: false, streaming: false, error: action.error };
    case "reset":
      return initialChatState;
    default:
      return state;
  }
}
