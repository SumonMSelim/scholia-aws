import { useRef, useState, type ComponentProps } from 'react'
import ReactMarkdown, { type Components, type ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeKatex from 'rehype-katex'
import rehypeHighlight from 'rehype-highlight'
import { common } from 'lowlight'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import latex from 'highlight.js/lib/languages/latex'
import matlab from 'highlight.js/lib/languages/matlab'
import scala from 'highlight.js/lib/languages/scala'
import { Check, Copy } from 'lucide-react'
import 'katex/dist/katex.min.css'
import { safeUrl } from '@/lib/safeUrl'

// rehype-highlight bundles lowlight's common set anyway, so the extras are only what course
// material adds on top: LaTeX, MATLAB, Scala and Dockerfiles.
const languages = { ...common, dockerfile, latex, matlab, scala }

// Model output is untrusted. KaTeX may not run \href or \htmlClass, and expansion
// is capped so a crafted macro cannot stall the tab.
const katexOptions = { trust: false, strict: 'ignore' as const, maxExpand: 1000, maxSize: 20, throwOnError: false }

function languageOf(node: ExtraProps['node']): string {
  const code = node?.children.find((child) => child.type === 'element' && child.tagName === 'code')
  const names = code?.type === 'element' ? code.properties.className : undefined
  const found = Array.isArray(names) ? names.find((name) => String(name).startsWith('language-')) : undefined
  return found ? String(found).slice('language-'.length) : ''
}

function CodeBlock({ node, children, ...rest }: ComponentProps<'pre'> & ExtraProps) {
  const pre = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)
  const language = languageOf(node)

  async function onCopy() {
    const text = pre.current?.textContent ?? ''
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="md-code">
      <div className="md-code-bar">
        <span>{language || 'text'}</span>
        <button type="button" className="md-code-copy" aria-label={copied ? 'Copied' : 'Copy code'} onClick={() => void onCopy()}>
          {copied ? <Check className="size-3.5" aria-hidden /> : <Copy className="size-3.5" aria-hidden />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre ref={pre} {...rest}>
        {children}
      </pre>
    </div>
  )
}

const components: Components = {
  pre: CodeBlock,
  a({ node: _node, href, children, ...rest }) {
    const safe = href ? safeUrl(href) : ''
    if (!safe) return <span>{children}</span>
    return (
      <a {...rest} href={safe} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
  // Images would fetch from wherever the model points. Show the alt text instead.
  img({ alt }) {
    return alt ? <span className="text-muted-foreground">[{alt}]</span> : null
  },
  table({ node: _node, ...rest }) {
    return (
      <div className="md-table">
        <table {...rest} />
      </div>
    )
  },
}

/** Renders assistant markdown. Raw HTML is never parsed, so tags in model output show as text. */
export default function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[
          [rehypeKatex, katexOptions],
          [rehypeHighlight, { languages, detect: false }],
        ]}
        components={components}
        urlTransform={safeUrl}
        skipHtml
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
