import { useState } from "react";
import { BarChart2, MessageSquare, Newspaper } from "lucide-react";
import { Separator } from "@/components/ui/separator";
import { TooltipProvider } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { ArticleList } from "./components/ArticleList";
import { ChatPanel } from "./components/ChatPanel";
import { StatsPanel } from "./components/StatsPanel";

type Tab = "chat" | "articles" | "stats";

const NAV: { id: Tab; label: string; Icon: React.ElementType }[] = [
  { id: "chat",     label: "智能问答", Icon: MessageSquare },
  { id: "articles", label: "文章管理", Icon: Newspaper },
  { id: "stats",    label: "统计",     Icon: BarChart2 },
];

export default function App() {
  const [tab, setTab] = useState<Tab>("chat");

  return (
    <TooltipProvider>
      <div className="flex h-screen overflow-hidden bg-background">
        {/* Sidebar */}
        <aside className="w-56 shrink-0 border-r flex flex-col bg-card">
          <div className="h-14 flex items-center px-5 shrink-0">
            <span className="font-semibold text-sm tracking-tight">Daily Info Agent</span>
          </div>
          <Separator />
          <nav className="flex-1 p-3 space-y-1">
            {NAV.map(({ id, label, Icon }) => (
              <button
                key={id}
                onClick={() => setTab(id)}
                className={cn(
                  "w-full flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors",
                  tab === id
                    ? "bg-accent text-accent-foreground font-medium"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                )}
              >
                <Icon className="w-4 h-4 shrink-0" />
                {label}
              </button>
            ))}
          </nav>
        </aside>

        {/* Main content */}
        <main className="flex-1 overflow-hidden">
          {tab === "chat" && <ChatPanel />}
          {tab === "articles" && (
            <div className="h-full overflow-y-auto">
              <div className="max-w-5xl mx-auto px-6 py-6">
                <ArticleList />
              </div>
            </div>
          )}
          {tab === "stats" && (
            <div className="h-full overflow-y-auto">
              <div className="max-w-4xl mx-auto px-6 py-6">
                <StatsPanel />
              </div>
            </div>
          )}
        </main>
      </div>
    </TooltipProvider>
  );
}
