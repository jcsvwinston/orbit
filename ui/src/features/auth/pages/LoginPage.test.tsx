import { afterEach, describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import LoginPage from './LoginPage'

// The login screen is the first one an operator sees and the one that says
// whose product this is. The logo travels on the served document as a meta
// tag, like the title, because no API call can carry it before sign-in.

function declareLogo(url: string) {
  const meta = document.createElement('meta')
  meta.name = 'nucleus-admin-logo'
  meta.content = url
  document.head.appendChild(meta)
}

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <LoginPage />
    </MemoryRouter>,
  )
}

afterEach(() => {
  document.head.querySelectorAll('meta[name="nucleus-admin-logo"]').forEach((m) => m.remove())
})

describe('LoginPage', () => {
  it('draws the logo the application declared', () => {
    declareLogo('https://cdn.example.test/brand/logo.svg')
    const { container } = renderLogin()
    const logo = container.querySelector('img')
    expect(logo).not.toBeNull()
    expect(logo).toHaveAttribute('src', 'https://cdn.example.test/brand/logo.svg')
    // The title names the product in text right below it, so the image is
    // decorative to a screen reader: an empty alt, present on purpose.
    expect(logo).toHaveAttribute('alt', '')
    expect(screen.getByRole('heading')).toBeInTheDocument()
    expect(container.querySelector('svg.lucide-shield')).toBeNull()
  })

  it('keeps the panel mark when nothing is declared', () => {
    const { container } = renderLogin()
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('svg.lucide-shield')).not.toBeNull()
  })

  it('gives the panel mark back when the logo does not load', () => {
    declareLogo('/static/missing-logo.svg')
    const { container } = renderLogin()
    const logo = container.querySelector('img')
    expect(logo).not.toBeNull()
    fireEvent.error(logo!)
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('svg.lucide-shield')).not.toBeNull()
  })
})
