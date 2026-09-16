/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // 🔴 Sabit hex DEGIL, CSS degiskeni: whitelabel eklentisi vurgu rengini
        // calisma aninda degistirebilsin diye. Varsayilan degerler styles.css
        // icindeki :root blogunda; eklenti kurulu degilse panel birebir eski
        // turuncu ile acilir.
        //
        // Sinif adlari DEGISMIYOR: bg-brand-600 / text-brand-500 vb. panelin
        // her yerinde aynen calisir. `<alpha-value>` de Tailwind'in opaklik
        // yardimcilarini (bg-brand-600/30) KORUR — bu olmadan yuzlerce yerdeki
        // yari saydam vurgu sessizce duz renge donerdi.
        brand: {
          50:  'rgb(var(--brand-50) / <alpha-value>)',
          100: 'rgb(var(--brand-100) / <alpha-value>)',
          200: 'rgb(var(--brand-200) / <alpha-value>)',
          300: 'rgb(var(--brand-300) / <alpha-value>)',
          400: 'rgb(var(--brand-400) / <alpha-value>)',
          500: 'rgb(var(--brand-500) / <alpha-value>)',
          600: 'rgb(var(--brand-600) / <alpha-value>)',
          700: 'rgb(var(--brand-700) / <alpha-value>)',
          800: 'rgb(var(--brand-800) / <alpha-value>)',
          900: 'rgb(var(--brand-900) / <alpha-value>)',
          950: 'rgb(var(--brand-950) / <alpha-value>)',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['JetBrains Mono', 'Menlo', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
}
