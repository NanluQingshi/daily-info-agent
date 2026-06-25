import { Plus, Trash2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Conversation } from "../types";

interface Props {
  conversations: Conversation[];
  activeId: string;
  onSelect: (localId: string) => void;
  onCreate: () => void;
  onDelete: (localId: string) => void;
}

export function ConversationList({
  conversations,
  activeId,
  onSelect,
  onCreate,
  onDelete,
}: Props) {
  return (
    <div className="w-48 shrink-0 border-r flex flex-col bg-card">
      {/* Header + New button */}
      <div className="h-14 shrink-0 flex items-center justify-between px-3 border-b">
        <span className="text-xs font-medium text-muted-foreground">对话列表</span>
        <button
          onClick={onCreate}
          className="p-1.5 rounded-md hover:bg-accent transition-colors"
          title="新建对话"
        >
          <Plus className="w-4 h-4" />
        </button>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto py-2 space-y-0.5 px-2">
        {conversations.map((conv) => (
          <div
            key={conv.localId}
            className={cn(
              "group flex items-center gap-1 px-2 py-2 rounded-md cursor-pointer text-sm transition-colors",
              conv.localId === activeId
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
            )}
            onClick={() => onSelect(conv.localId)}
          >
            <span className="flex-1 truncate text-xs leading-snug">
              {conv.title}
            </span>
            {/* Delete — only visible on hover */}
            <button
              onClick={(e) => {
                e.stopPropagation();
                onDelete(conv.localId);
              }}
              className="shrink-0 p-0.5 rounded opacity-0 group-hover:opacity-100 hover:text-destructive transition-all"
              title="删除对话"
            >
              <Trash2 className="w-3 h-3" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
