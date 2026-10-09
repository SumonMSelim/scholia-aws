import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { safeUrl } from '@/lib/safeUrl'
import Markdown from './Markdown'
import { MessageText } from './MessageText'

describe('Markdown', () => {
  it('renders headings, lists, tables and highlighted code', () => {
    const text = [
      '## Sliding window',
      '',
      '- first',
      '- second',
      '',
      '| seq | ack |',
      '| --- | --- |',
      '| 1000 | 1500 |',
      '',
      '```python',
      'def ack(seq):',
      '    return seq + 500',
      '```',
    ].join('\n')
    const { container } = render(<Markdown text={text} />)
    expect(screen.getByRole('heading', { level: 2, name: 'Sliding window' })).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(within(screen.getByRole('table')).getByRole('cell', { name: '1500' })).toBeInTheDocument()
    const code = container.querySelector('pre code')
    expect(code).toHaveClass('language-python')
    expect(code?.querySelector('.hljs-keyword')).toHaveTextContent('def')
    expect(screen.getByText('python')).toBeInTheDocument()
  })

  it('highlights LaTeX, which is outside the common language set', () => {
    const { container } = render(<Markdown text={'```latex\n\\begin{enumerate}\n\\item One\n\\end{enumerate}\n```'} />)
    expect(container.querySelector('pre code')).toHaveClass('language-latex')
    expect(container.querySelector('pre code [class^="hljs-"]')).not.toBeNull()
  })

  it('copies a code block and confirms it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    render(<Markdown text={'```go\nfmt.Println("hi")\n```'} />)
    await userEvent.click(screen.getByRole('button', { name: 'Copy code' }))
    expect(writeText).toHaveBeenCalledWith('fmt.Println("hi")\n')
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })

  it('renders inline and block math with KaTeX', () => {
    const { container } = render(<Markdown text={'Throughput is $W/RTT$.\n\n$$\nT = \\frac{W}{RTT}\n$$'} />)
    expect(container.querySelectorAll('.katex').length).toBeGreaterThanOrEqual(2)
    expect(container.querySelector('.katex-display')).not.toBeNull()
  })

  it('keeps an unclosed fence readable while it streams', () => {
    const { container } = render(<Markdown text={'Steps:\n\n```bash\nls -la'} />)
    expect(container.querySelector('pre')).toHaveTextContent('ls -la')
  })

  it('never renders raw HTML from the model', () => {
    const { container } = render(<Markdown text={'<script>alert(1)</script>\n\n<img src=x onerror="alert(1)">\n\nok'} />)
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('[onerror]')).toBeNull()
    expect(screen.getByText('ok')).toBeInTheDocument()
  })

  it('drops unsafe links and opens safe ones in a new tab', () => {
    render(<Markdown text={'[bad](javascript:alert(1)) [rel](/admin) [good](https://example.com/a) [mail](mailto:a@b.c)'} />)
    expect(screen.getByText('bad').closest('a')).toBeNull()
    expect(screen.getByText('rel').closest('a')).toBeNull()
    const good = screen.getByRole('link', { name: 'good' })
    expect(good).toHaveAttribute('href', 'https://example.com/a')
    expect(good).toHaveAttribute('target', '_blank')
    expect(good).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByRole('link', { name: 'mail' })).toHaveAttribute('href', 'mailto:a@b.c')
  })

  it('shows image alt text instead of loading the image', () => {
    const { container } = render(<Markdown text={'![diagram](https://example.com/x.png)'} />)
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('[diagram]')).toBeInTheDocument()
  })

  it.each([
    ['https://a.b/c', 'https://a.b/c'],
    ['mailto:x@y.z', 'mailto:x@y.z'],
    ['javascript:alert(1)', ''],
    ['data:text/html,x', ''],
    ['/relative', ''],
  ])('safeUrl(%s)', (input, want) => {
    expect(safeUrl(input)).toBe(want)
  })
})

describe('MessageText', () => {
  it('loads the renderer lazily and shows markdown', async () => {
    render(<MessageText text={'**bold** text'} />)
    expect(await screen.findByText('bold', { selector: 'strong' })).toBeInTheDocument()
  })
})
