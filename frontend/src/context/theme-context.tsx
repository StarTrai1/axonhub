import { createContext, useContext, useEffect, useState } from 'react';

type Theme = 'dark' | 'light' | 'system';
type ColorScheme = 'blue' | 'green' | 'purple' | 'orange' | 'red' | 'black' | 'cream' | 'claude' | 'starry';

// Keep the accepted values in sync with the pre-paint script in index.html.
function readPreference<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  try {
    const value = localStorage.getItem(key) as T | null;
    if (value && allowed.includes(value)) return value;
  } catch {
    // Continue with defaults when storage is unavailable.
  }
  return fallback;
}

function writePreference(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Theme switching still works for the current page.
  }
}

type ThemeProviderProps = {
  children: React.ReactNode;
  defaultTheme?: Theme;
  defaultColorScheme?: ColorScheme;
  storageKey?: string;
  colorSchemeStorageKey?: string;
};

type ThemeProviderState = {
  theme: Theme;
  colorScheme: ColorScheme;
  setTheme: (theme: Theme) => void;
  setColorScheme: (colorScheme: ColorScheme) => void;
};

const initialState: ThemeProviderState = {
  theme: 'system',
  colorScheme: 'blue',
  setTheme: () => null,
  setColorScheme: () => null,
};

const ThemeProviderContext = createContext<ThemeProviderState>(initialState);

export function ThemeProvider({
  children,
  defaultTheme = 'system',
  defaultColorScheme = 'claude',
  storageKey = 'axonhub-ui-theme',
  colorSchemeStorageKey = 'axonhub-ui-color-scheme',
  ...props
}: ThemeProviderProps) {
  const [theme, _setTheme] = useState<Theme>(() => readPreference(storageKey, defaultTheme, ['dark', 'light', 'system']));
  const [colorScheme, _setColorScheme] = useState<ColorScheme>(() =>
    readPreference(colorSchemeStorageKey, defaultColorScheme, ['blue', 'green', 'purple', 'orange', 'red', 'black', 'cream', 'claude', 'starry'])
  );

  useEffect(() => {
    const root = window.document.documentElement;
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');

    const applyTheme = (theme: Theme, colorScheme: ColorScheme) => {
      // Remove existing theme and color scheme classes
      root.classList.remove('light', 'dark', 'blue', 'green', 'purple', 'orange', 'red', 'black', 'cream', 'claude', 'starry');

      const systemTheme = mediaQuery.matches ? 'dark' : 'light';
      const effectiveTheme = theme === 'system' ? systemTheme : theme;

      // Add theme and color scheme classes with transition
      root.style.transition = 'background-color 0.3s ease, color 0.3s ease';
      root.classList.add(effectiveTheme, colorScheme);

      // Remove transition after animation completes
      setTimeout(() => {
        root.style.transition = '';
      }, 300);
    };

    const handleChange = () => {
      if (theme === 'system') {
        applyTheme('system', colorScheme);
      }
    };

    applyTheme(theme, colorScheme);

    mediaQuery.addEventListener('change', handleChange);

    return () => mediaQuery.removeEventListener('change', handleChange);
  }, [theme, colorScheme]);

  const setTheme = (theme: Theme) => {
    writePreference(storageKey, theme);
    _setTheme(theme);
  };

  const setColorScheme = (colorScheme: ColorScheme) => {
    writePreference(colorSchemeStorageKey, colorScheme);
    _setColorScheme(colorScheme);
  };

  const value = {
    theme,
    colorScheme,
    setTheme,
    setColorScheme,
  };

  return (
    <ThemeProviderContext.Provider {...props} value={value}>
      {children}
    </ThemeProviderContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export const useTheme = () => {
  const context = useContext(ThemeProviderContext);

  if (context === undefined) throw new Error('useTheme must be used within a ThemeProvider');

  return context;
};
