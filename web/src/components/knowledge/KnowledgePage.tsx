import { useState, type FormEvent } from 'react'
import { BookOpen, Globe, Plus } from 'lucide-react'
import { createCourse, errorText, type Course } from '@/lib/api'
import { fieldClass } from '@/lib/field'
import { navigate } from '@/lib/router'
import { Link } from '@/components/Link'
import { useAppData } from '@/components/app/appDataContext'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export function KnowledgePage() {
  const { courses, coursesError, chats, setCourses } = useAppData()
  const [creating, setCreating] = useState(false)
  const [title, setTitle] = useState('')
  const [isPublic, setIsPublic] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const rows = courses ?? []
  const yours = rows.filter((course) => course.mine !== false)
  const shared = rows.filter((course) => course.mine === false)
  const chatCount = (id: string) => (chats ?? []).filter((chat) => chat.course_id === id).length

  async function onCreate(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const course = await createCourse(title.trim(), { public: isPublic })
      setCourses((prev) => [...prev, course])
      setTitle('')
      setIsPublic(false)
      setCreating(false)
      navigate(`/knowledge/${encodeURIComponent(course.id)}`)
    } catch (err) {
      setError(errorText(err, 'could not create course'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-8 px-4 py-8 sm:px-6 lg:px-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Knowledge</h1>
          <p className="mt-1 text-sm text-muted-foreground">Courses hold your lectures, slides, notes, and assignments. Chats answer from one course.</p>
        </div>
        {!creating ? (
          <Button type="button" onClick={() => setCreating(true)}>
            <Plus aria-hidden />
            New course
          </Button>
        ) : null}
      </div>

      {creating ? (
        <Card className="max-w-lg">
          <CardHeader>
            <CardTitle>New course</CardTitle>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={(event) => void onCreate(event)}>
              <label className="block space-y-1.5 text-sm font-medium">
                Course title
                <input aria-label="Course title" autoFocus className={fieldClass} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="e.g. Computer Networks" />
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input aria-label="Public course" type="checkbox" className="size-4 rounded border-input accent-primary" checked={isPublic} onChange={(event) => setIsPublic(event.target.checked)} />
                Public: every account can read and ask this course.
              </label>
              {error ? (
                <Alert variant="destructive">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              ) : null}
              <div className="flex gap-2">
                <Button type="submit" disabled={busy || title.trim() === ''}>
                  Create course
                </Button>
                <Button type="button" variant="ghost" onClick={() => setCreating(false)}>
                  Cancel
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      ) : null}

      {coursesError ? (
        <Alert variant="destructive">
          <AlertDescription>{coursesError}</AlertDescription>
        </Alert>
      ) : null}
      {courses === null ? <p className="text-sm text-muted-foreground">Loading courses…</p> : null}

      {courses !== null ? (
        <section aria-labelledby="your-courses" className="space-y-3">
          <h2 id="your-courses" className="text-sm font-medium text-muted-foreground">
            Your courses
          </h2>
          {yours.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border px-6 py-12 text-center">
              <BookOpen className="mx-auto size-6 text-muted-foreground" aria-hidden />
              <p className="mt-3 text-sm text-muted-foreground">No courses yet. Create one, then add your lecture files.</p>
            </div>
          ) : (
            <CourseGrid courses={yours} chatCount={chatCount} />
          )}
        </section>
      ) : null}

      {shared.length > 0 ? (
        <section aria-labelledby="public-courses" className="space-y-3">
          <h2 id="public-courses" className="text-sm font-medium text-muted-foreground">
            Public courses
          </h2>
          <CourseGrid courses={shared} chatCount={chatCount} />
        </section>
      ) : null}
    </div>
  )
}

function CourseGrid({ courses, chatCount }: { courses: Course[]; chatCount: (id: string) => number }) {
  return (
    <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
      {courses.map((course) => {
        const count = chatCount(course.id)
        return (
          <li key={course.id}>
            <Link
              to={`/knowledge/${encodeURIComponent(course.id)}`}
              className="flex h-full flex-col gap-3 rounded-xl border border-border bg-card p-4 transition-colors hover:border-ring/50 hover:bg-muted/40 focus-visible:border-ring focus-visible:outline-none"
            >
              <span className="flex items-start justify-between gap-2">
                <span className="font-medium">{course.title}</span>
                {course.public ? (
                  <Badge variant="secondary">
                    <Globe aria-hidden />
                    Public
                  </Badge>
                ) : null}
              </span>
              <span className="mt-auto text-xs text-muted-foreground">
                {count === 0 ? 'No chats yet' : `${count} chat${count === 1 ? '' : 's'}`}
              </span>
            </Link>
          </li>
        )
      })}
    </ul>
  )
}
