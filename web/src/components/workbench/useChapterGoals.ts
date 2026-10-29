import { useEffect, useMemo, useState } from 'react'
import { parseChapterGoals, type ChapterGoal } from './story-ledger-parser'

interface ChapterPlanReference {
  path: string
}

interface UseChapterGoalsOptions {
  projectId?: string
  chapterPlans?: readonly ChapterPlanReference[] | null
  readFile?: (path: string) => Promise<string>
}

/** Loads and parses the two most recent chapter-group plans for all writing hosts. */
export function useChapterGoals({ projectId, chapterPlans, readFile }: UseChapterGoalsOptions) {
  const chapterPlanPaths = useMemo(
    () => (chapterPlans ?? []).slice(-2).map((plan) => plan.path),
    [chapterPlans],
  )
  const [chapterPlanContents, setChapterPlanContents] = useState<ReadonlyMap<string, string>>(new Map())

  useEffect(() => {
    let cancelled = false
    if (!readFile || chapterPlanPaths.length === 0) {
      setChapterPlanContents(new Map())
      return () => { cancelled = true }
    }
    void Promise.all(chapterPlanPaths.map(async (path) => {
      try {
        return [path, await readFile(path)] as const
      } catch (cause) {
        console.warn('[useChapterGoals.ts] failed to load chapter-group plan for chapter goals', {
          projectId,
          path,
          cause,
        })
        return null
      }
    })).then((loaded) => {
      if (cancelled) return
      setChapterPlanContents(new Map(loaded.filter((item): item is readonly [string, string] => Boolean(item))))
    })
    return () => { cancelled = true }
  }, [chapterPlanPaths, projectId, readFile])

  const chapterGoals = useMemo(() => {
    const result = new Map<string, ChapterGoal>()
    for (const [path, content] of chapterPlanContents) {
      for (const [anchor, goal] of parseChapterGoals(content, path)) result.set(anchor, goal)
    }
    return result
  }, [chapterPlanContents])

  return { chapterPlanPaths, chapterPlanContents, chapterGoals }
}
