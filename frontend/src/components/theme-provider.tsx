/**
 * shadcn/ui themes through a `.dark` class on <html>, so something has to put it there.
 *
 * next-themes is already a dependency (the sonner toaster reads the active theme from
 * it), so it is the one that gets to own the class rather than a second mechanism.
 */

import { ThemeProvider as NextThemeProvider, type ThemeProviderProps } from 'next-themes'

export function ThemeProvider({ children, ...props }: ThemeProviderProps) {
  return (
    <NextThemeProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
      storageKey="marketplace.theme"
      {...props}
    >
      {children}
    </NextThemeProvider>
  )
}
