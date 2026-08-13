import { useCallback, useEffect, useState } from "react";
import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";
import { cn } from "@/lib/utils";

// ── Toast types ──────────────────────────────────────────────────────────

type ToastVariant = "success" | "error" | "info";

interface Toast {
  id: number;
  variant: ToastVariant;
  message: string;
}

let toastId = 0;
let pushToast: ((variant: ToastVariant, message: string) => void) | null = null;

/**
 * showToast displays a transient toast notification anywhere in the app.
 * Must be called after <ToastProvider> has mounted.
 */
export function showToast(variant: ToastVariant, message: string) {
  pushToast?.(variant, message);
}

// ── Provider ─────────────────────────────────────────────────────────────

const VARIANTS: Record<ToastVariant, { icon: React.ElementType; classes: string }> = {
  success: { icon: CheckCircle2, classes: "border-green-200 bg-green-50 text-green-800" },
  error: { icon: AlertCircle, classes: "border-red-200 bg-red-50 text-red-800" },
  info: { icon: Info, classes: "border-blue-200 bg-blue-50 text-blue-800" },
};

const TOAST_TTL = 4000; // ms before auto-dismiss
const MAX_TOASTS = 5;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const remove = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  useEffect(() => {
    pushToast = (variant, message) => {
      const id = ++toastId;
      setToasts((prev) => [...prev.slice(-(MAX_TOASTS - 1)), { id, variant, message }]);
      window.setTimeout(() => remove(id), TOAST_TTL);
    };
    return () => {
      pushToast = null;
    };
  }, [remove]);

  return (
    <>
      {children}
      {/* Toast container — fixed top-right, above modals */}
      <div className="fixed top-4 right-4 z-[100] flex flex-col gap-2 w-80 max-w-[calc(100vw-2rem)]">
        {toasts.map((t) => {
          const { icon: Icon, classes } = VARIANTS[t.variant];
          return (
            <div
              key={t.id}
              role="status"
              className={cn(
                "flex items-start gap-2 rounded-xl border px-3 py-2.5 shadow-lg text-sm animate-in slide-in-from-top-2",
                classes
              )}
            >
              <Icon className="w-4 h-4 shrink-0 mt-0.5" />
              <span className="flex-1 break-words">{t.message}</span>
              <button
                onClick={() => remove(t.id)}
                className="shrink-0 text-current/50 hover:text-current transition-colors"
                aria-label="关闭"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          );
        })}
      </div>
    </>
  );
}
