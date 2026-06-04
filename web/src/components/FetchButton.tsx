import { useRef, useState } from "react";

interface Props {
  onComplete?: () => void;
}

interface StageState {
  status: "idle" | "running" | "done" | "error";
  detail: string;
}

const STAGES: { key: string; label: string }[] = [
  { key: "fetch", label: "抓取新闻" },
  { key: "process", label: "AI 处理" },
  { key: "verify", label: "验证" },
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
      const data: {
        stage: string;
        status: string;
        message: string;
      } = JSON.parse(event.data);

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
        <button
          onClick={handleStart}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors"
        >
          立即抓取
        </button>
      )}

      {showProgress && (
        <div className="bg-white border border-slate-200 rounded-xl p-3 w-52 shadow-sm">
          <div className="space-y-2">
            {STAGES.map(({ key, label }) => {
              const s = stages[key];
              return (
                <div key={key} className="flex items-center gap-2">
                  <StageIcon status={s.status} />
                  <span
                    className={`text-xs truncate ${
                      s.status === "idle" ? "text-slate-300" : "text-slate-600"
                    }`}
                  >
                    {s.status === "done" ? s.detail : label}
                  </span>
                </div>
              );
            })}
          </div>

          {doneMsg && (
            <div className="mt-2 pt-2 border-t border-slate-100 flex items-center justify-between">
              <span className="text-xs text-green-600 font-medium">{doneMsg}</span>
              <button
                onClick={reset}
                className="text-slate-400 hover:text-slate-600 text-base leading-none ml-2"
                aria-label="关闭"
              >
                ×
              </button>
            </div>
          )}
        </div>
      )}

      {errorMsg && (
        <p className="text-xs text-red-600">{errorMsg}</p>
      )}
    </div>
  );
}

function StageIcon({ status }: { status: StageState["status"] }) {
  if (status === "done") {
    return <span className="w-4 h-4 flex items-center justify-center text-green-500 text-xs shrink-0">✓</span>;
  }
  if (status === "running") {
    return (
      <span className="w-4 h-4 shrink-0 flex items-center justify-center">
        <span className="w-3 h-3 border-2 border-blue-500 border-t-transparent rounded-full animate-spin block" />
      </span>
    );
  }
  return <span className="w-4 h-4 flex items-center justify-center text-slate-200 text-xs shrink-0">○</span>;
}
