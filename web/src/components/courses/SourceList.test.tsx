import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { Source } from '@/lib/api'
import { SourceList } from './SourceList'

function source(status: Source['status'], extra: Partial<Source> = {}): Source {
  return { id: status, name: `${status}.txt`, content_type: 'text/plain', status, ...extra }
}

describe('SourceList', () => {
  it('shows an empty state', () => {
    render(<SourceList sources={[]} />)
    expect(screen.getByText(/No files yet/)).toBeInTheDocument()
  })

  it('shows in-progress, ready, and failed rows', () => {
    render(
      <SourceList
        sources={[
          source('queued'),
          source('processing'),
          source('ready'),
          source('failed', { failure_reason: 'bad magic' }),
        ]}
      />,
    )
    expect(screen.getByText('Queued')).toBeInTheDocument()
    expect(screen.getByText('Processing')).toBeInTheDocument()
    expect(screen.getByText('Ready')).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('bad magic')).toBeInTheDocument()
  })
})
