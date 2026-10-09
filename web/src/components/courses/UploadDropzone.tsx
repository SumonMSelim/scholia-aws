import { useRef, useState } from 'react'
import { UploadCloud } from 'lucide-react'
import { uploadAccept, uploadHint } from '@/lib/uploads'

export function UploadDropzone({
  disabled,
  error,
  onFile,
}: {
  disabled?: boolean
  error?: string | null
  onFile: (file: File) => void
}) {
  const [over, setOver] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  function takeFiles(files: FileList | null) {
    if (!files) return
    for (const file of Array.from(files)) onFile(file)
  }

  return (
    <div
      className={`rounded-xl border-2 border-dashed px-4 py-10 text-center text-sm transition-colors ${
        over ? 'border-primary bg-primary/5' : 'border-input bg-background hover:border-ring/40'
      } ${disabled ? 'opacity-60' : ''}`}
      onDragOver={(event) => {
        event.preventDefault()
        if (!disabled) setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(event) => {
        event.preventDefault()
        setOver(false)
        if (disabled) return
        takeFiles(event.dataTransfer.files)
      }}
    >
      <UploadCloud className="mx-auto size-8 text-muted-foreground" aria-hidden />
      <p className="mt-3 text-muted-foreground">
        Drag files here or{' '}
        <button
          type="button"
          className="font-medium text-primary underline-offset-4 hover:underline disabled:pointer-events-none"
          disabled={disabled}
          onClick={() => inputRef.current?.click()}
        >
          browse
        </button>
      </p>
      <p className="mt-1 text-xs text-muted-foreground">{uploadHint}</p>
      <input
        ref={inputRef}
        aria-label="Upload files"
        className="sr-only"
        type="file"
        accept={uploadAccept}
        multiple
        disabled={disabled}
        onChange={(event) => {
          takeFiles(event.target.files)
          event.target.value = ''
        }}
      />
      {error ? (
        <p role="alert" className="mt-3 text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}
