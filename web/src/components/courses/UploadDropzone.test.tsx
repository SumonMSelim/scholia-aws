import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { UploadDropzone } from './UploadDropzone'

describe('UploadDropzone', () => {
  it('shows an error', () => {
    render(<UploadDropzone error="content type is not allowed" onFile={() => {}} />)
    expect(screen.getByRole('alert')).toHaveTextContent('content type is not allowed')
  })

  it('accepts a dropped file and a chosen file', async () => {
    const onFile = vi.fn()
    render(<UploadDropzone onFile={onFile} />)
    const file = new File(['hello notes\n'], 'notes.txt', { type: 'text/plain' })
    const zone = screen.getByText(/Drag files here/).closest('div')
    if (!zone) throw new Error('drop zone missing')
    fireEvent.dragOver(zone)
    fireEvent.dragLeave(zone)
    fireEvent.drop(zone, { dataTransfer: { files: [file] } })
    expect(onFile).toHaveBeenCalledWith(file)

    await userEvent.click(screen.getByRole('button', { name: 'browse' }))
    await userEvent.upload(screen.getByLabelText('Upload files'), file)
    expect(onFile).toHaveBeenCalledTimes(2)
  })

  it('ignores drops while disabled and a drop with no files', () => {
    const onFile = vi.fn()
    const view = render(<UploadDropzone disabled onFile={onFile} />)
    const zone = screen.getByText(/Drag files here/).closest('div')
    if (!zone) throw new Error('drop zone missing')
    fireEvent.dragOver(zone)
    expect(zone.className).not.toContain('border-primary')
    fireEvent.drop(zone, { dataTransfer: { files: [new File(['x'], 'a.txt')] } })
    expect(screen.getByRole('button', { name: 'browse' })).toBeDisabled()
    expect(screen.getByLabelText('Upload files')).toBeDisabled()
    view.unmount()

    render(<UploadDropzone onFile={onFile} />)
    const live = screen.getByText(/Drag files here/).closest('div')
    if (!live) throw new Error('drop zone missing')
    fireEvent.dragOver(live)
    expect(live.className).toContain('border-primary')
    fireEvent.drop(live, { dataTransfer: { files: null } })
    expect(live.className).not.toContain('border-primary')
    expect(onFile).not.toHaveBeenCalled()
  })

  it('passes every file of a multi-file drop', () => {
    const onFile = vi.fn()
    render(<UploadDropzone onFile={onFile} />)
    const zone = screen.getByText(/Drag files here/).closest('div')
    if (!zone) throw new Error('drop zone missing')
    const files = [new File(['a'], 'a.pdf', { type: 'application/pdf' }), new File(['b'], 'b.md', { type: 'text/markdown' })]
    fireEvent.drop(zone, { dataTransfer: { files } })
    expect(onFile.mock.calls.map((call) => (call[0] as File).name)).toEqual(['a.pdf', 'b.md'])
  })
})
