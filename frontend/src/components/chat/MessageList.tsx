import { useEffect, useRef } from "react";
import { MessageBubble } from "~/components/chat/MessageBubble";
import { ScrollArea } from "~/components/ui/scroll-area";
import type { ChatMessage } from "~/lib/types";

const SAMPLE_MESSAGES: ChatMessage[] = [
  {
    id: "1",
    role: "user",
    content: "Can you explain the project structure?",
    timestamp: "10:30 AM",
  },
  {
    id: "2",
    role: "assistant",
    content:
      "Sure! The project follows a standard monorepo structure with separate backend and frontend directories.\n\nThe backend is written in Go and handles:\n- HTTP API server\n- SQLite database for persistence\n- WebSocket connections for real-time updates\n\nThe frontend is a React SPA with:\n- TanStack Router for navigation\n- Zustand for state management\n- Tailwind CSS for styling",
    timestamp: "10:30 AM",
  },
  {
    id: "3",
    role: "user",
    content: "Can you refactor the auth middleware to use JWT tokens?",
    timestamp: "10:32 AM",
  },
  {
    id: "4",
    role: "assistant",
    content:
      "I'll refactor the auth middleware. Let me start by reading the current implementation.\n\nI've updated the middleware to use JWT tokens. The changes include:\n- New JWT validation function\n- Token refresh logic\n- Updated error handling for expired tokens",
    timestamp: "10:33 AM",
  },
];

export function MessageList() {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  return (
    <ScrollArea className="flex-1">
      <div className="p-4 space-y-4">
        {SAMPLE_MESSAGES.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        <div ref={bottomRef} />
      </div>
    </ScrollArea>
  );
}
