// The prompt's Preview (D §10.7), loaded with it: react-markdown without raw HTML (D §11), so a
// prompt can't put markup or scripts on the page. Its headings sit under the "Prompt" heading
// (h1 → h5, the rest h6), and links open outside the editor.
import Markdown, { type Components } from 'react-markdown';

const components: Components = {
  h1: ({ node: _n, ...props }) => <h5 className="mt-3 mb-1.5 text-lg font-medium first:mt-0" {...props} />,
  h2: ({ node: _n, ...props }) => <h6 className="mt-3 mb-1.5 text-base font-medium first:mt-0" {...props} />,
  h3: ({ node: _n, ...props }) => <h6 className="mt-3 mb-1 text-[15px] font-medium first:mt-0" {...props} />,
  h4: ({ node: _n, ...props }) => <h6 className="mt-3 mb-1 text-[15px] font-medium first:mt-0" {...props} />,
  h5: ({ node: _n, ...props }) => <h6 className="mt-3 mb-1 text-[15px] font-medium first:mt-0" {...props} />,
  h6: ({ node: _n, ...props }) => <h6 className="mt-3 mb-1 text-[15px] font-medium first:mt-0" {...props} />,
  a: ({ node: _n, ...props }) => (
    <a className="text-ink underline underline-offset-3 hover:text-run" target="_blank" rel="noreferrer" {...props} />
  ),
};

export default function MarkdownPreview({ text }: { text: string }) {
  return (
    <div
      className={[
        'max-h-[640px] overflow-auto px-4 py-3 text-[15px] leading-relaxed text-ink',
        '[&_p]:my-2 [&_ul]:my-2 [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:my-2 [&_ol]:list-decimal [&_ol]:pl-5',
        '[&_code]:rounded-chip [&_code]:bg-shell [&_code]:px-1 [&_code]:font-mono [&_code]:text-[13px]',
        '[&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-control [&_pre]:bg-shell [&_pre]:p-3',
        '[&_blockquote]:border-l-2 [&_blockquote]:border-rule [&_blockquote]:pl-3 [&_blockquote]:text-muted',
      ].join(' ')}
    >
      <Markdown skipHtml components={components}>
        {text}
      </Markdown>
    </div>
  );
}
