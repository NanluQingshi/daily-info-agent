import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

interface Props {
  content: string;
  /** Extra class names applied to the wrapper div. */
  className?: string;
  /** When true a blinking cursor is appended (used while streaming). */
  streaming?: boolean;
}

/**
 * MarkdownContent renders a Markdown string with GitHub-flavored extensions
 * (tables, strikethrough, task lists, autolinks).
 *
 * Inline styles are intentionally minimal — Tailwind prose-like classes are
 * applied per element so the output fits the existing design system.
 */
export function MarkdownContent({ content, className, streaming }: Props) {
  return (
    <div className={cn("text-sm leading-relaxed min-w-0", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // ── Block elements ───────────────────────────────────────────────
          p: ({ children }) => (
            <p className="mb-2 last:mb-0 whitespace-pre-wrap">{children}</p>
          ),
          h1: ({ children }) => (
            <h1 className="text-base font-bold mt-4 mb-2 first:mt-0">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-sm font-bold mt-3 mb-1.5 first:mt-0">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-sm font-semibold mt-2 mb-1 first:mt-0">{children}</h3>
          ),
          ul: ({ children }) => (
            <ul className="list-disc pl-5 mb-2 space-y-0.5">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="list-decimal pl-5 mb-2 space-y-0.5">{children}</ol>
          ),
          li: ({ children }) => <li className="leading-relaxed">{children}</li>,
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-muted-foreground/30 pl-3 my-2 text-muted-foreground italic">
              {children}
            </blockquote>
          ),
          hr: () => <hr className="my-3 border-border" />,

          // ── Tables (remark-gfm) ──────────────────────────────────────────
          table: ({ children }) => (
            <div className="overflow-x-auto my-2">
              <table className="w-full text-xs border-collapse">{children}</table>
            </div>
          ),
          thead: ({ children }) => (
            <thead className="bg-muted/50">{children}</thead>
          ),
          th: ({ children }) => (
            <th className="border border-border px-2 py-1 text-left font-medium">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="border border-border px-2 py-1">{children}</td>
          ),

          // ── Code ─────────────────────────────────────────────────────────
          code: ({ className: langClass, children, ...rest }) => {
            const isBlock = langClass?.startsWith("language-");
            if (isBlock) {
              return (
                <code
                  className="block bg-muted rounded-md px-3 py-2 text-xs font-mono overflow-x-auto whitespace-pre my-2"
                  {...rest}
                >
                  {children}
                </code>
              );
            }
            return (
              <code
                className="bg-muted rounded px-1 py-0.5 text-xs font-mono"
                {...rest}
              >
                {children}
              </code>
            );
          },
          pre: ({ children }) => <>{children}</>,

          // ── Inline ───────────────────────────────────────────────────────
          strong: ({ children }) => (
            <strong className="font-semibold">{children}</strong>
          ),
          em: ({ children }) => <em className="italic">{children}</em>,
          del: ({ children }) => (
            <del className="line-through text-muted-foreground">{children}</del>
          ),
          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary underline underline-offset-2 hover:opacity-80"
            >
              {children}
            </a>
          ),
        }}
      >
        {content}
      </ReactMarkdown>

      {/* Blinking cursor appended while streaming */}
      {streaming && (
        <span className="inline-block w-0.5 h-4 ml-0.5 bg-foreground animate-pulse align-text-bottom" />
      )}
    </div>
  );
}
