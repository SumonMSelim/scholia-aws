import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { Select, type SelectOption } from './select'

const options: SelectOption[] = [
  { value: 'gpt', label: 'GPT-4.1 mini', group: 'OpenAI' },
  { value: 'nova', label: 'Nova Lite', group: 'Bedrock' },
  { value: 'haiku', label: 'Claude Haiku', group: 'Bedrock', disabled: true },
]

function Harness({ onChange }: { onChange?: (value: string) => void }) {
  const [value, setValue] = useState('gpt')
  return (
    <Select
      label="Model"
      value={value}
      options={options}
      onValueChange={(next) => {
        setValue(next)
        onChange?.(next)
      }}
    />
  )
}

describe('Select', () => {
  it('shows the selected label and picks an option with the mouse', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    const trigger = screen.getByRole('combobox', { name: 'Model' })
    expect(trigger).toHaveTextContent('GPT-4.1 mini')
    await userEvent.click(trigger)
    expect(await screen.findByRole('listbox')).toBeInTheDocument()
    expect(screen.getByText('OpenAI')).toBeInTheDocument()
    expect(screen.getByText('Bedrock')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('option', { name: 'Nova Lite' }))
    expect(onChange).toHaveBeenCalledWith('nova')
    expect(trigger).toHaveTextContent('Nova Lite')
  })

  it('shows the placeholder for an unknown value and respects disabled', () => {
    render(<Select label="Course" value="" options={[]} placeholder="No courses" disabled onValueChange={() => {}} />)
    const trigger = screen.getByRole('combobox', { name: 'Course' })
    expect(trigger).toHaveTextContent('No courses')
    expect(trigger).toHaveAttribute('data-disabled')
  })
  it('works from the keyboard and returns focus to the trigger', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    const trigger = screen.getByRole('combobox', { name: 'Model' })
    trigger.focus()
    await userEvent.keyboard('{ArrowDown}')
    expect(await screen.findByRole('listbox')).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('listbox')).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
    expect(onChange).not.toHaveBeenCalled()

    await userEvent.keyboard('{ArrowDown}')
    await screen.findByRole('listbox')
    await userEvent.keyboard('{ArrowDown}{Enter}')
    await waitFor(() => expect(onChange).toHaveBeenCalledWith('nova'))
    expect(trigger).toHaveFocus()
  })
})
