import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { place } from '@/lib/cite'
import { SourceList } from './SourceList'

describe('SourceList', () => {
  it('numbers passages as the answer cites them, then web results', async () => {
    render(
      <SourceList
        citations={[
          { chunk_id: 'a', source_id: 's1', locators: [{ kind: 'text', start: 0, end: 9 }], section: 'Lecture 3 › Subnetting', excerpt: 'Borrow host bits.' },
          { chunk_id: 'b', source_id: 'gone', locators: [] },
        ]}
        web={[{ title: 'QUIC', url: 'https://www.example.org/quic' }]}
        names={{ s1: '03-ip-routing.md' }}
      />,
    )
    const list = screen.getByRole('region', { name: 'Sources' })
    // A text offset is not shown; a file that is no longer listed keeps a generic name.
    expect(within(list).getByRole('button', { name: /^1\.03-ip-routing\.md.*Subnetting$/ })).toBeInTheDocument()
    const old = within(list).getByRole('button', { name: /^2\.Course file$/ })
    await userEvent.click(old)
    expect(screen.getByRole('region', { name: 'Source 2' })).toHaveTextContent('saved before passages were kept')
    expect(within(list).getByText('W1.')).toBeInTheDocument()
    expect(within(list).getByRole('link', { name: /QUIC/ })).toHaveAttribute('href', 'https://www.example.org/quic')
  })

  it('plays a recorded lecture at the cited moment', async () => {
    render(
      <SourceList
        citations={[
          { chunk_id: 'a', source_id: 'lec4', locators: [{ kind: 'time', start_ms: 754_000, end_ms: 760_000 }] },
          { chunk_id: 'b', source_id: 'audio', locators: [{ kind: 'time', start_ms: 1000, end_ms: 2000 }] },
          { chunk_id: 'c', source_id: 'notes', locators: [{ kind: 'page', page: 3 }] },
        ]}
        web={[]}
        names={{ lec4: 'Lecture 04 - Hashing - transcript.vtt', audio: 'memo.mp3', notes: 'notes.pdf' }}
        videos={{ lec4: 'Nu8YGneFCWE', notes: 'Nu8YGneFCWE' }}
      />,
    )
    // Only a lecture moment with a recording can be watched.
    const watch = screen.getByRole('button', { name: 'Watch 12:34' })
    expect(screen.getAllByRole('button', { name: /^Watch/ })).toHaveLength(1)
    expect(document.querySelector('iframe')).toBeNull()

    await userEvent.click(watch)
    const player = screen.getByTitle('Lecture 04 - Hashing - transcript.vtt at 12:34')
    expect(player).toHaveAttribute('src', 'https://www.youtube-nocookie.com/embed/Nu8YGneFCWE?start=752&autoplay=1&rel=0')
    expect(screen.getByRole('button', { name: 'Close' })).toHaveAttribute('aria-pressed', 'true')

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(document.querySelector('iframe')).toBeNull()
  })

  it('renders nothing without sources', () => {
    const { container } = render(<SourceList citations={[]} web={[]} names={{}} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('names the page, slide or lecture moment', () => {
    expect(place([{ kind: 'text', start: 1, end: 2 }, { kind: 'page', page: 4 }])).toBe('p. 4')
    expect(place([{ kind: 'slide', slide: 2 }])).toBe('slide 2')
    expect(place([{ kind: 'time', start_ms: 754000, end_ms: 760000 }])).toBe('12:34')
    expect(place([{ kind: 'text', start: 1, end: 2 }])).toBe('')
  })
})
