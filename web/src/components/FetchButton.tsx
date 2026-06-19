import { useRef, useState } from "react";
import { CheckCircle2, Circle, Loader2, RefreshCw, X, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";

interface Props {
  onComplete?: () => void;
}

interface StageState {
  status: "idle" | "running" | "done" | "error";
  detail: string;
}

const STAGES: { key: string; label: string }[] = [
  { key: "fetch",   label: "抓取新闻" },
  { key: "process", label: "AI 处理" },
  { key: "verify",  label: "验证" },
  { key: "publish", label: "发布" },
];

const initStages = (): Record<string, StageState> =>
  Object.fromEntries(STAGES.map((s) => [s.key, { status: "idle", detail: s.label }]));

export function FetchButton({ onComplete }: Props) {
  const [running, setRunning] = useState(false);
  const [stages, setStages] = useState<Record<string, StageState>>(initStages());
  const [doneMsg, setDoneMsg] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const esRef = useRef<EventSource | null>(null);

  const reset = () => {
    setStages(initStages());
    setDoneMsg(null);
    setErrorMsg(null);
  };

  const handleStart = () => {
    if (running) return;
    reset();
    setRunning(true);

    const es = new EventSource("/api/fetch/stream");
    esRef.current = es;

    es.onmessage = (event) => {
      const data: { stage: string; status: string; message: string } = JSON.parse(event.data);

      if (data.stage === "done") {
        es.close();
        setRunning(false);
        setDoneMsg(data.message);
        onComplete?.();
        return;
      }
      if (data.stage === "error") {
        es.close();
        setRunning(false);
        setErrorMsg(data.message);
        return;
      }
      setStages((prev) => {
        if (!(data.stage in prev)) return prev;
        return {
          ...prev,
          [data.stage]: {
            status: data.status === "running" ? "running" : "done",
            detail: data.status === "done" ? data.message : prev[data.stage].detail,
          },
        };
      });
    };

    es.onerror = () => {
      es.close();
      setRunning(false);
      setErrorMsg("连接中断，请重试");
    };
  };

  const showProgress = running || doneMsg !== null;

  return (
    <div className="flex flex-col items-end gap-2">
      {!showProgress && (
        <Button onClick={handleStart} size="sm" className="gap-2">
          <RefreshCw className="w-3.5 h-3.5" />
          立即抓取
        </Button>
      )}

      {showProgress && (
        <div className="bg-card border rounded-xl p-3 w-52 shadow-sm space-y-2">
          {STAGES.map(({ key, label }) => {
            const s = stages[key];
            return (
              <div key={key} className="flex items-center gap-2">
                {s.status === "idle" && <Circle className="w-3.5 h-3.5 text-muted-foreground/30 shrink-0" />}
                {s.status === "running" && <Loader2 className="w-3.5 h-3.5 text-primary shrink-0 animate-spin" />}
                {s.status === "done" && <CheckCircle2 className="w-3.5 h-3.5 text-green-500 shrink-0" />}
                {s.status === "error" && <XCircle className="w-3.5 h-3.5 text-destructive shrink-0" />}
                <span className={`text-xs truncate ${s.status === "idle" ? "text-muted-foreground/40" : "text-foreground"}`}>
                  {s.status === "done" ? s.detail : label}
                </span>
              </div>
            );
          })}
          {doneMsg && (
            <div className="flex items-center justify-between pt-1.5 border-t">
              <span className="text-xs text-green-600 font-medium">{doneMsg}</span>
              <button onClick={reset} className="text-muted-foreground hover:text-foreground ml-2">
                <X className="w-3.5 h-3.5" />
              </button>
            </div>
          )}
        </div>
      )}

      {errorMsg && <p className="text-xs text-destructive">{errorMsg}</p>}
    </div>
  );
}

