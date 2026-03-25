import ReactMarkdown from 'react-markdown';

interface Props {
  content: string;
  title: string;
}

export function MarkdownViewer({ content, title }: Props) {
  return (
    <div style={{ border: '1px solid #e5e7eb', borderRadius: '8px', padding: '1rem', marginBottom: '1rem' }}>
      <h3 style={{ marginTop: 0 }}>{title}</h3>
      <ReactMarkdown>{content}</ReactMarkdown>
    </div>
  );
}
