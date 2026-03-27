import { useState } from 'react';
import ReactMarkdown from 'react-markdown';

interface Props {
  content: string;
}

function CollapsiblePre({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = text.split('\n');
  const THRESHOLD = 12;
  const needsTruncation = lines.length > THRESHOLD;
  const displayText =
    !expanded && needsTruncation ? lines.slice(0, THRESHOLD).join('\n') : text;

  return (
    <pre className="bg-muted/50 rounded p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap my-2">
      {displayText}
      {needsTruncation && (
        <>
          {'\n'}
          <button
            className="text-blue-400 hover:underline text-xs font-sans cursor-pointer"
            onClick={() => setExpanded((e) => !e)}
          >
            {expanded ? 'Show less' : `Show more (${lines.length - THRESHOLD} lines)`}
          </button>
        </>
      )}
    </pre>
  );
}

export function MarkdownViewer({ content }: Props) {
  return (
    <div className="px-4 py-3 text-sm text-foreground">
      <ReactMarkdown
        components={{
          h1: ({ children }) => (
            <h1 className="text-lg font-semibold text-foreground mt-4 mb-2">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-base font-semibold text-foreground mt-3 mb-1.5">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-sm font-semibold text-foreground mt-2 mb-1">{children}</h3>
          ),
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer" className="text-blue-400 hover:underline">
              {children}
            </a>
          ),
          code: ({ children }) => (
            <code className="bg-muted text-xs font-mono px-1 py-0.5 rounded">{children}</code>
          ),
          pre: ({ children }) => {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            const codeEl = children as React.ReactElement<{ children: any }>;
            const raw = codeEl?.props?.children;
            const text = Array.isArray(raw) ? raw.join('') : String(raw ?? '');
            return <CollapsiblePre text={text} />;
          },
          ul: ({ children }) => (
            <ul className="pl-5 space-y-1 my-2 list-disc">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="pl-5 space-y-1 my-2 list-decimal">{children}</ol>
          ),
          p: ({ children }) => <p className="my-1.5 text-sm">{children}</p>,
          strong: ({ children }) => (
            <strong className="font-semibold text-foreground">{children}</strong>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
