/** Extensions the API accepts. Shown in the uploader and passed to the file input. */
export const uploadExtensions = [
  '.pdf',
  '.pptx',
  '.txt',
  '.md',
  '.tex',
  '.bib',
  '.png',
  '.jpg',
  '.jpeg',
  '.gif',
  '.webp',
  '.vtt',
  '.srt',
  '.json',
] as const

export const uploadAccept = uploadExtensions.join(',')

export const uploadHint = 'Slides (PPTX, PDF), notes (TXT, MD, TEX, BIB), captions (VTT, SRT) and images'
