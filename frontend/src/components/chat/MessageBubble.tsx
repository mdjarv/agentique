import { Bot, User } from "lucide-react";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import type { ChatMessage } from "~/lib/types";

interface MessageBubbleProps {
  message: ChatMessage;
}

export function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === "user";

  return (
    <div className={`flex gap-3 ${isUser ? "flex-row-reverse" : "flex-row"}`}>
      <Avatar className="h-8 w-8 shrink-0">
        <AvatarFallback className={isUser ? "bg-primary text-primary-foreground" : "bg-muted"}>
          {isUser ? <User className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
        </AvatarFallback>
      </Avatar>
      <div
        className={`max-w-[75%] rounded-lg px-4 py-2 ${
          isUser ? "bg-primary text-primary-foreground" : "bg-muted"
        }`}
      >
        <p className="text-sm whitespace-pre-wrap">{message.content}</p>
        <p
          className={`text-xs mt-1 ${
            isUser ? "text-primary-foreground/70" : "text-muted-foreground"
          }`}
        >
          {message.timestamp}
        </p>
      </div>
    </div>
  );
}
