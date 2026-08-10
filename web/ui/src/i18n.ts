import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import cs from "./locales/cs.json";
import en from "./locales/en.json";

const saved = localStorage.getItem("kc.lang");
const browser = navigator.language?.startsWith("cs") ? "cs" : "en";

void i18n.use(initReactI18next).init({
  resources: {
    cs: { translation: cs },
    en: { translation: en },
  },
  lng: saved || browser,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
});

export default i18n;
