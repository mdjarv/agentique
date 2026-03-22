export interface Project {
  id: string;
  name: string;
  path: string;
  default_model: string;
  default_permission_mode: string;
  default_system_prompt: string;
  created_at: string;
  updated_at: string;
}

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: string;
}

export interface Session {
  id: string;
  name: string;
  state: "idle" | "running";
}
