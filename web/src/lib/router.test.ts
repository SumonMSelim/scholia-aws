import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { matchRoute, navigate, useLocation } from './router'

describe('matchRoute', () => {
  it.each([
    ['/', { name: 'home' }],
    ['/chat', { name: 'chat', chatId: null }],
    ['/chat/h%201', { name: 'chat', chatId: 'h 1' }],
    ['/knowledge', { name: 'knowledge', courseId: null }],
    ['/knowledge/c1', { name: 'knowledge', courseId: 'c1' }],
    ['/settings', { name: 'settings' }],
    ['/settings/x', { name: 'notFound' }],
    ['/chat/a/b', { name: 'notFound' }],
    ['/nope', { name: 'notFound' }],
  ])('matches %s', (path, route) => {
    expect(matchRoute(path)).toEqual(route)
  })
})

describe('navigate', () => {
  afterEach(() => window.history.replaceState(null, '', '/'))

  it('updates the location and follows back navigation', () => {
    window.history.replaceState(null, '', '/')
    const { result } = renderHook(() => useLocation())
    expect(result.current.path).toBe('/')
    act(() => navigate('/chat/?course=c1'))
    expect(result.current.path).toBe('/chat')
    expect(result.current.query.get('course')).toBe('c1')
    act(() => navigate('/settings', { replace: true }))
    expect(result.current.path).toBe('/settings')
    act(() => {
      window.history.pushState(null, '', '/knowledge')
      window.dispatchEvent(new PopStateEvent('popstate'))
    })
    expect(result.current.path).toBe('/knowledge')
    const length = window.history.length
    act(() => navigate('/knowledge'))
    expect(window.history.length).toBe(length)
  })
})
