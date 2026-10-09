/**
 * Public copy for the landing page. The page renders it, and the build writes
 * the same text into index.html for search engines and agents that do not run
 * JavaScript. Keep sentences short and words common: many readers are not
 * native English speakers.
 */

export const siteUrl = 'https://scholia-aws.mol.la'
export const repoUrl = 'https://github.com/SumonMSelim/scholia-aws'

export const description =
  'Upload slides, PDFs, notes and lecture captions. Ask questions and get answers with the page, slide or lecture moment they came from. Practice with mock exams.'

export const hero = {
  title: 'Study from your own lectures and slides.',
  body:
    'Upload slides, PDFs, notes and lecture captions. ' +
    'Ask a question and get an answer from your course, with the page, slide or lecture moment it came from. ' +
    'Then test yourself with a mock exam.',
  demoNote: 'Try MIT 6.006 Introduction to Algorithms without signing up. Guest data is deleted after 48 hours.',
}

export type FeatureKey = 'sources' | 'recordings' | 'slides' | 'exams' | 'help' | 'web'

export const features: { key: FeatureKey; title: string; body: string }[] = [
  {
    key: 'sources',
    title: 'Answers with sources',
    body: 'Every answer shows where it came from: the page, the slide or the minute in the lecture. Open it and check.',
  },
  {
    key: 'recordings',
    title: 'Watch the lecture moment',
    body: 'When an answer cites a lecture that has a video, press Watch. The video starts at the moment the answer came from.',
  },
  {
    key: 'slides',
    title: 'Slides, PDFs and notes',
    body: 'PowerPoint and PDF files are read page by page, with their diagrams, tables and formulas. Notes and LaTeX work too.',
  },
  {
    key: 'exams',
    title: 'Mock exams',
    body: 'Ask for a practice exam. You get one question at a time, a grade for each answer and the topics to review.',
  },
  {
    key: 'help',
    title: 'Help with assignments',
    body: 'Work through a problem step by step with the methods from your course, or send a draft and get clear feedback.',
  },
  {
    key: 'web',
    title: 'Web search when you need it',
    body: 'If your files do not cover a topic, Scholia can search the web. Web sources are always marked as web sources.',
  },
]

export type StepKey = 'course' | 'files' | 'ask' | 'test'

export const steps: { key: StepKey; title: string; body: string }[] = [
  { key: 'course', title: 'Create a course', body: 'Give it a name, like Computer Networks.' },
  { key: 'files', title: 'Add your files', body: 'Drop in slides, PDFs, notes or lecture captions.' },
  { key: 'ask', title: 'Ask anything', body: 'Use your own words and your own language. Each answer shows its sources.' },
  { key: 'test', title: 'Test yourself', body: 'Take a mock exam and see which topics need more work.' },
]

/** What the uploader accepts, in words a student uses. Matches uploadExtensions. */
export const formats: { kind: string; files: string }[] = [
  { kind: 'Slides and documents', files: 'PowerPoint (PPTX), PDF' },
  { kind: 'Notes', files: 'Text, Markdown, LaTeX and Overleaf, BibTeX' },
  { kind: 'Transcripts and captions', files: 'VTT, SRT' },
  { kind: 'Photos and screenshots', files: 'PNG, JPG, GIF, WebP' },
]

export const faq: { q: string; a: string }[] = [
  {
    q: 'Is Scholia free?',
    a: 'Yes. You can try the demo without signing up. With a free account you can create your own courses and chat every day, up to a daily limit.',
  },
  {
    q: 'What files can I upload?',
    a: 'PowerPoint slides, PDFs, notes in text, Markdown or LaTeX, lecture captions (VTT, SRT) and photos or screenshots (PNG, JPG, GIF, WebP).',
  },
  {
    q: 'Can I watch the part of the lecture an answer comes from?',
    a: 'Yes, when the lecture has a published video, like the demo course. Press Watch under the answer and the video starts at that moment.',
  },
  {
    q: 'How is Scholia different from ChatGPT?',
    a: 'Scholia answers from your own course files first and shows the page, slide or minute for each answer. If your files do not cover something, it tells you, or searches the web and marks those sources clearly.',
  },
  {
    q: 'Does it make things up?',
    a: 'It answers from your files and shows the source, so you can check every answer. If nothing in your course covers the question, it says so instead of guessing.',
  },
  {
    q: 'Can it do my assignment for me?',
    a: 'It can work through a problem step by step and explain each step with your course material. If you only want a hint, ask it to explain the idea and point you to the lecture.',
  },
  {
    q: 'Is my data private?',
    a: 'Your courses are private unless you choose to make one public. Guest data is deleted after 48 hours. If you connect your own AI provider, your key is encrypted and never shown again.',
  },
  {
    q: 'Is Scholia open source?',
    a: 'Yes. The code is public on GitHub at github.com/SumonMSelim/scholia-aws.',
  },
]
