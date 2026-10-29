import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ImageAPIProfilesEditor } from './ImageAPIProfilesEditor'
import { ModelProfilesEditor } from './ModelProfilesEditor'

vi.mock('./api', () => ({
  fetchModelCatalog: vi.fn().mockResolvedValue({ providers: [], protocols: [] }),
}))

describe('model profile identity editing', () => {
  it('keeps language profile references stable when the model changes twice', async () => {
    const user = userEvent.setup()
    const onProfilesChange = vi.fn()
    const onDefaultProfileChange = vi.fn()
    const endpoints = [{ id: 'endpoint', provider: 'openai', protocol: 'openai-chat-completions', base_url: 'https://example.test/v1', api_key: 'key' }]
    const view = render(
      <ModelProfilesEditor
        endpoints={endpoints}
        effectiveEndpoints={endpoints}
        profiles={[{ id: 'writer-v1', endpoint_id: 'endpoint', model: 'writer-v1' }]}
        effectiveProfiles={[]}
        defaultProfileID="writer-v1"
        effectiveDefaultProfileID="writer-v1"
        onDefaultProfileChange={onDefaultProfileChange}
        onEndpointsChange={vi.fn()}
        onProfilesChange={onProfilesChange}
      />,
    )

    await user.click(screen.getByRole('button', { name: /连接 1/ }))
    await user.click(screen.getByRole('button', { name: /默认模型writer-v1/ }))
    const input = screen.getByDisplayValue('writer-v1')
    fireEvent.change(input, { target: { value: 'writer-v2' } })
    fireEvent.change(input, { target: { value: 'writer-v3' } })

    expect(onProfilesChange).toHaveBeenLastCalledWith([
      { id: 'writer-v1', endpoint_id: 'endpoint', model: 'writer-v3' },
    ])
    expect(onDefaultProfileChange).not.toHaveBeenCalledWith('writer-v2')
    expect(onDefaultProfileChange).not.toHaveBeenCalledWith('writer-v3')
    view.unmount()
  })
})

describe('image profile identity editing', () => {
  it('keeps image profile references stable when the model changes twice', async () => {
    const user = userEvent.setup()
    const onProfilesChange = vi.fn()
    const onDefaultProfileChange = vi.fn()
    const endpoints = [{ id: 'endpoint', provider: 'custom', protocol: 'openai-images', base_url: 'http://localhost:8000/v1' }]
    const view = render(
      <ImageAPIProfilesEditor
        endpoints={endpoints}
        effectiveEndpoints={endpoints}
        profiles={[{ id: 'image-v1', endpoint_id: 'endpoint', model: 'image-v1' }]}
        effectiveProfiles={[]}
        defaultProfileID="image-v1"
        effectiveDefaultProfileID="image-v1"
        onDefaultProfileChange={onDefaultProfileChange}
        onEndpointsChange={vi.fn()}
        onProfilesChange={onProfilesChange}
      />,
    )

    await user.click(screen.getByRole('button', { name: /连接 1/ }))
    await user.click(screen.getByRole('button', { name: /默认模型image-v1/ }))
    const input = screen.getByDisplayValue('image-v1')
    fireEvent.change(input, { target: { value: 'image-v2' } })
    fireEvent.change(input, { target: { value: 'image-v3' } })

    expect(onProfilesChange).toHaveBeenLastCalledWith([
      { id: 'image-v1', endpoint_id: 'endpoint', model: 'image-v3' },
    ])
    expect(onDefaultProfileChange).not.toHaveBeenCalledWith('image-v2')
    expect(onDefaultProfileChange).not.toHaveBeenCalledWith('image-v3')
    view.unmount()
  })
})
