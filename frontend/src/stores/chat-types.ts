import type { SessionInfo } from "~/lib/generated-types";

// --- Auto-approve mode ---

export type AutoApproveMode = "manual" | "auto" | "fullAuto";

// --- Tool content blocks ---

export interface ToolContentBlock {
  type: "text" | "image";
  text?: string;
  mediaType?: string;
  url?: string;
}

// --- Discriminated union: ChatEvent ---

interface BaseChatEvent {
  id: string;
  timestamp?: number;
  parentToolUseId?: string;
}

export interface TextEvent extends BaseChatEvent {
  type: "text";
  content: string;
}

export interface ThinkingEvent extends BaseChatEvent {
  type: "thinking";
  content: string;
  signature?: string;
}

export interface ToolUseEvent extends BaseChatEvent {
  type: "tool_use";
  toolId: string;
  toolName: string;
  toolInput: unknown;
  category?: string;
}

export interface ToolResultEvent extends BaseChatEvent {
  type: "tool_result";
  toolId: string;
  contentBlocks?: ToolContentBlock[];
}

export interface ResultEvent extends BaseChatEvent {
  type: "result";
  cost?: number;
  duration?: number;
  usage?: { inputTokens: number; outputTokens: number };
  stopReason?: string;
  contextWindow?: number;
  inputTokens?: number;
  outputTokens?: number;
}

export interface ErrorEvent extends BaseChatEvent {
  type: "error";
  content: string;
  fatal?: boolean;
  errorType?: string;
  retryAfterSecs?: number;
}

export interface RateLimitEvent extends BaseChatEvent {
  type: "rate_limit";
  status?: string;
  utilization?: number;
  resetsAt?: number;
  rateLimitType?: string;
}

export interface StreamEvent extends BaseChatEvent {
  type: "stream";
}

export interface CompactStatusEvent extends BaseChatEvent {
  type: "compact_status";
  status?: string;
}

export interface CompactBoundaryEvent extends BaseChatEvent {
  type: "compact_boundary";
  trigger?: string;
  preTokens?: number;
}

export interface ContextManagementEvent extends BaseChatEvent {
  type: "context_management";
}

export interface UserMessageEvent extends BaseChatEvent {
  type: "user_message";
  content?: string;
  fromUser?: boolean;
  messageId?: string;
  deliveryStatus?: "sending" | "delivered";
  attachments?: Attachment[];
}

export interface MessageDeliveryEvent extends BaseChatEvent {
  type: "message_delivery";
  messageId?: string;
  deliveryStatus?: "sending" | "delivered";
}

export interface AgentMessageEvent extends BaseChatEvent {
  type: "agent_message";
  direction?: "sent" | "received";
  fromUser?: boolean;
  senderSessionId?: string;
  senderName?: string;
  targetSessionId?: string;
  targetName?: string;
  content?: string;
  messageType?: "plan" | "progress" | "done" | "message";
}

export interface TaskEvent extends BaseChatEvent {
  type: "task";
  toolUseId?: string;
  taskSubtype?: "task_started" | "task_progress" | "task_notification";
  taskDescription?: string;
  taskType?: string;
  taskSummary?: string;
  taskStatus?: string;
  lastToolName?: string;
  totalTokens?: number;
  toolUses?: number;
  durationMs?: number;
}

export interface AgentResultEvent extends BaseChatEvent {
  type: "agent_result";
  status?: string;
  agentId?: string;
  agentType?: string;
  contentBlocks?: ToolContentBlock[];
  totalDurationMs?: number;
  totalTokens?: number;
  totalToolUseCount?: number;
}

export type ChatEvent =
  | TextEvent
  | ThinkingEvent
  | ToolUseEvent
  | ToolResultEvent
  | ResultEvent
  | ErrorEvent
  | RateLimitEvent
  | StreamEvent
  | CompactStatusEvent
  | CompactBoundaryEvent
  | ContextManagementEvent
  | UserMessageEvent
  | MessageDeliveryEvent
  | AgentMessageEvent
  | TaskEvent
  | AgentResultEvent;

export type ChatEventType = ChatEvent["type"];

// --- Attachments ---

export interface Attachment {
  id: string;
  name: string;
  mimeType: string;
  dataUrl: string; // data:...;base64,... for sending/history
  previewUrl?: string; // blob: URL for local preview (not persisted)
}

// --- Turns ---

export interface Turn {
  id: string;
  prompt: string;
  attachments: Attachment[];
  events: ChatEvent[];
  complete: boolean;
}

// --- Session state ---

export type SessionState = "idle" | "running" | "done" | "failed" | "stopped" | "merging";

export type SessionMetadata = Omit<SessionInfo, "state" | "mergeStatus"> & {
  state: SessionState;
  mergeStatus?: "clean" | "conflicts" | "unknown";
  gitRefreshedAt?: number;
  icon?: string;
};

// --- Approvals / Questions ---

export interface PendingApproval {
  approvalId: string;
  toolName: string;
  input: unknown;
}

export interface QuestionOption {
  label: string;
  description?: string;
}

export interface Question {
  question: string;
  header?: string;
  options?: QuestionOption[];
  multiSelect?: boolean;
}

export interface PendingQuestion {
  questionId: string;
  questions: Question[];
}

// --- Todos ---

export interface TodoItem {
  content: string;
  activeForm?: string;
  status: "completed" | "in_progress" | "pending";
}

// --- Context usage ---

export interface ContextUsage {
  contextWindow: number;
  inputTokens: number;
  outputTokens: number;
}

// --- Session data ---

export interface SessionData {
  meta: SessionMetadata;
  turns: Turn[];
  /** Events for the in-progress (last, incomplete) turn that haven't been merged yet.
   *  Kept separate so `turns` stays referentially stable during streaming,
   *  preventing unnecessary re-renders of completed TurnBlocks. */
  streamingEvents: ChatEvent[];
  /** True when the full turn history has been loaded from the server.
   *  False when only a tail cache of recent turns is retained after eviction. */
  historyComplete: boolean;
  hasUnseenCompletion: boolean;
  hasUnreadChannelMessage: boolean;
  pendingApproval: PendingApproval | null;
  pendingQuestion: PendingQuestion | null;
  planMode: boolean;
  autoApproveMode: AutoApproveMode;
  todos: TodoItem[] | null;
  contextUsage: ContextUsage | null;
  compacting: boolean;
}
