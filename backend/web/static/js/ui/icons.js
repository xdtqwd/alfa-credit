// Иконки дизайн-системы Альфа-Банка (Core Components): @alfalab/icons-glyph 2.293.0, размер M 24×24, MIT.
// Знак — @alfalab/icons-logo 1.63.0 (AlfaBankSignMIcon), MIT. Здесь только SVG-пути нужных иконок.

const GLYPHS = {
  plus: '<path d="M11 11H5v2h6v6h2v-6h6v-2h-6V5h-2v6z"/>', // Add
  calc: '<path d="M20 4a2 2 0 00-2-2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V4zm-8 15a1.5 1.5 0 100-3 1.5 1.5 0 000 3zm4.5-8c.69 0 1.25.56 1.25 1.25v5.5a1.25 1.25 0 11-2.5 0v-5.5c0-.69.56-1.25 1.25-1.25zM6 6a1 1 0 011-1h10a1 1 0 011 1v2H6V6zm1.5 13a1.5 1.5 0 100-3 1.5 1.5 0 000 3zm6-6.5a1.5 1.5 0 11-3 0 1.5 1.5 0 013 0zm-4.5 0a1.5 1.5 0 11-3 0 1.5 1.5 0 013 0z" fill-rule="evenodd" clip-rule="evenodd"/>', // Calculator
  bolt: '<path d="M6.5 12l2-9h7l-3 7 5 1-10 10 3-8-4-1z"/>', // Lightning
  cycle: '<path d="M15 8h2.277c-1.5-2-3.121-3-5.5-3C8.606 5 6 6.5 5.067 12H3c.5-6 4.494-9 8.777-9 2.827 0 5.5 1.5 7 3.5L19 4h2v6h-6V8zm-6 8H6.723c1.5 2 3.121 3 5.5 3 3.171 0 5.777-1.5 6.71-7H21c-.5 6-4.494 9-8.777 9-2.827 0-5.5-1.5-7-3.5L5 20H3v-6h6v2z"/>', // ArrowsCw
  bank: '<path d="M22.5 10V8L12 3 1.5 8v2h1.75v8h.005l-.895.447a2 2 0 00-1.105 1.79V21.5h22v-1.264a2 2 0 00-1.106-1.789L20.755 18h-.005v-8h1.75zm-4.25 0h-2.5v8h2.5v-8zm-5 0h-2.5v8h2.5v-8zm-5 0h-2.5v8h2.5v-8z" fill-rule="evenodd" clip-rule="evenodd"/>', // Bank
  doc: '<path d="M5 2a1 1 0 00-1 1v18a1 1 0 001 1h14a1 1 0 001-1V9.815a3 3 0 00-3.299-2.985l-1.458.146a.2.2 0 01-.219-.22L15.17 5.3A3 3 0 0012.185 2H5zm2 9h5v2H7v-2zm0 4h9v2H7v-2z" fill-rule="evenodd" clip-rule="evenodd"/>', // DocumentLines
  chat: '<path d="M22 10.335C22 4.917 17.835 2 12 2 6.164 2 1.997 4.917 1.997 10.335c0 5.417 4.167 8.31 9.168 8.31v2.133c0 .422 0 .634.132.84a.987.987 0 00.48.352c.235.065.391.017.703-.08C17 20.336 22 15.752 22 10.334z" fill-rule="evenodd" clip-rule="evenodd"/>', // Bubble
  back: '<path d="M10 12l5.5 5.5L14 19l-7-7 7-7 1.5 1.5L10 12z"/>', // ChevronBack
  forward: '<path d="M14 12l-5.5 5.5L10 19l7-7-7-7-1.5 1.5L14 12z"/>', // ChevronForward
  chev: '<path d="M12 14L6.5 8.5 5 10l7 7 7-7-1.5-1.5L12 14z"/>', // ChevronDown
  user: '<path d="M21 22v-2.051c-.052-.78-.395-1.384-.892-1.976-1.133-1.351-3.716-3.152-8.108-3.152-4.008 0-6.68 1.694-7.946 3.055-.602.647-.995 1.18-1.054 2.073V22h18zM12 2c3 0 4 2.588 4 5.128s-1.773 4.616-4 4.616c-2.227 0-4-2.029-4-4.616C8 4.541 9 2 12 2z"/>', // User
  lock: '<path d="M7 7a5 5 0 0110 0v3h2v10a2 2 0 01-2 2H7a2 2 0 01-2-2V10h2V7zm8 0v3H9V7a3 3 0 116 0zm-3 11a2 2 0 100-4 2 2 0 000 4z" fill-rule="evenodd" clip-rule="evenodd"/>', // LockClosed
  copy: '<path d="M10 2a2 2 0 00-2 2h10v14a2 2 0 002-2V4a2 2 0 00-2-2h-8z"/><path d="M6 6a2 2 0 00-2 2v12a2 2 0 002 2h8a2 2 0 002-2V8a2 2 0 00-2-2H6zm8 2H6v12h8V8z" fill-rule="evenodd" clip-rule="evenodd"/>', // CopyLine
  out: '<path d="M10 3.344a1 1 0 011.287-.958L19.43 4.83a.8.8 0 01.57.766v12.81a.8.8 0 01-.57.766l-8.143 2.443A1 1 0 0110 20.656V3.344zM14 12a1 1 0 11-2 0 1 1 0 012 0z" fill-rule="evenodd" clip-rule="evenodd"/><path d="M5 5a1 1 0 00-1 1v12a1 1 0 001 1h3V5H5z"/>', // LogOut
  card: '<path d="M3 9h18V8a2 2 0 00-2-2H5a2 2 0 00-2 2v1z"/><path d="M3 16a2 2 0 002 2h14a2 2 0 002-2v-5H3v5zm5-3H5v1.5h3V13z" fill-rule="evenodd" clip-rule="evenodd"/>', // Card
  check: '<path d="M20 7.519L9.903 17 5 12.327l1.448-1.503 3.47 3.308L18.569 6 20 7.519z" fill-rule="evenodd" clip-rule="evenodd"/>', // Checkmark
  checkCircle: '<path d="M22 12c0 5.523-4.477 10-10 10S2 17.523 2 12 6.477 2 12 2s10 4.477 10 10zm-11.5 4L17 9.5 15.5 8l-5 5-2-2L7 12.5l3.5 3.5z" fill-rule="evenodd" clip-rule="evenodd"/>', // CheckmarkCircle
  menu: '<path d="M3 13.25h18v-2.5H3v2.5zm0-7.5v2.5h18v-2.5H3zm0 12.5h18v-2.5H3v2.5z"/>', // Burger
  cross: '<path d="M10.586 12L6 16.5 7.5 18l4.5-4.586L16.5 18l1.5-1.5-4.586-4.5L18 7.5 16.5 6 12 10.586 7.5 6 6 7.5l4.586 4.5z"/>', // Cross
  arrow: '<path d="M16.5 13H5v-2h11.5L13 7.5 14.5 6l5.5 6-5.5 6-1.5-1.5 3.5-3.5z"/>', // ArrowRight
  home: '<path d="M3.586 13c-.914 0-1.349-1.125-.673-1.74l8.077-7.343a1.5 1.5 0 012.018 0l8.078 7.343c.676.615.24 1.74-.673 1.74h-1.414v6.5a1.5 1.5 0 01-1.5 1.5h-11A1.5 1.5 0 015 19.5V13H3.586zM10 13h4v5h-4v-5z" fill-rule="evenodd" clip-rule="evenodd"/><path d="M16 3.545h2V7l-2-2V3.545z"/>', // House
  clock: '<path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10zM11 7v5a3 3 0 001.2 2.4l2.6 1.7 1.2-1.6-2.6-1.7a1 1 0 01-.4-.8V7h-2z" fill-rule="evenodd" clip-rule="evenodd"/>', // Clock
  office: '<path d="M12 8.68l-1 3h1.99l-.95-3H12z"/><path d="M22.5 9.248V7.25l-10.5-5-10.5 5v2h2l-.001 8.004s-2 0-2 1.996v1.5h21v-1.5c0-2.001-2-2.001-2-2.001v-8h2zM12.08 7.045c.81 0 1.045.48 1.25 1.09l2.14 6.315h-1.565l-.5-1.5H10.57l-.5 1.5h-1.5l2.22-6.315c.21-.61.48-1.09 1.29-1.09zM16 17.5H8V16h8v1.5z"/>', // Office
  phone: '<path d="M7.167 2.795a1.015 1.015 0 00-1.542.132L4.406 4.629A4.833 4.833 0 003.5 7.45c.003.808.213 1.621.65 2.358 2.77 4.664 5.411 7.252 10.03 10.028 1.648.99 3.692.84 5.205-.244l1.688-1.21c.51-.367.574-1.102.132-1.543l-2.873-2.875a1.012 1.012 0 00-1.24-.149l-2.383 1.925-6.447-6.446L10.19 6.91c.241-.4.18-.91-.148-1.24L7.167 2.796z" fill-rule="evenodd" clip-rule="evenodd"/>', // Phone
  mail: '<path d="M3 5a1 1 0 00-1 1v1l10 4.5L22 7V6a1 1 0 00-1-1H3zm9 8.5L22 9v9a1 1 0 01-1 1H3a1 1 0 01-1-1V9l10 4.5z" fill-rule="evenodd" clip-rule="evenodd"/>', // Mail
  calendar: '<path d="M9 12v2H7v-2h2zm0 6v-2H7v2h2zm2-6h2v2h-2v-2zm2 4h-2v2h2v-2zm4-4v2h-2v-2h2z"/><path d="M6 2h2v2h8V2h2v2h3v16a3 3 0 01-3 3H6a3 3 0 01-3-3V4h3V2zm13 7H5v11a1 1 0 001 1h12a1 1 0 001-1V9z" fill-rule="evenodd" clip-rule="evenodd"/>', // Calendar
  info: '<path d="M12 2C6.477 2 2 6.477 2 12s4.477 10 10 10 10-4.477 10-10S17.523 2 12 2zm1 15v-6h-2v6h2zm-1-8a1.274 1.274 0 100-2.548A1.274 1.274 0 0012 9z" fill-rule="evenodd" clip-rule="evenodd"/>', // InformationCircle
  alert: '<path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10zm-1-9V7h2v6h-2zm0 4v-2h2v2h-2z" fill-rule="evenodd" clip-rule="evenodd"/>', // AlertCircle
  wallet: '<path d="M18.283 6.619L5.859 7.734c-.69.088-.859.493-.859.99V9h14a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V8.724C3 7.231 3.606 6 5.576 5.754l10.534-.946a2 2 0 012.158 1.71l.015.1zM15 15.01c0 1.12.891 1.989 2.011 1.989S19 16.131 19 15.011 18.131 13 17.011 13A2 2 0 0015 15.011z" fill-rule="evenodd" clip-rule="evenodd"/>', // Wallet
  percent: '<path d="M11 7a4 4 0 11-8 0 4 4 0 018 0zM7 9a2 2 0 100-4 2 2 0 000 4zm-.359 10.963l-1.534-1.284L17.36 4.038l1.534 1.284L6.64 19.963zM13 17a4 4 0 118 0 4 4 0 01-8 0zm4-2a2 2 0 100 4 2 2 0 000-4z" fill-rule="evenodd" clip-rule="evenodd"/>', // Percent
  shield: '<path d="M3.086 6.5C4.077 18.364 12 21 12 21s7.923-2.636 8.914-14.5c.04-.485.07-.984.085-1.5a.977.977 0 00-.985-1H3.986A.977.977 0 003 5c.017.516.046 1.015.086 1.5zm6.268 9.578C7.784 14.336 6.049 11.386 5.6 6.5H12v11.738l-.132-.076a10.879 10.879 0 01-2.514-2.084z"/>', // Shield
  briefcase: '<path d="M8 5a2 2 0 012-2h4a2 2 0 012 2v2h4a2 2 0 012 2v2H2V9a2 2 0 012-2h4V5zm6 0v2h-4V5h4zm8 8H2v6a2 2 0 002 2h16a2 2 0 002-2v-6zm-8 0a2 2 0 11-4 0h4z" fill-rule="evenodd" clip-rule="evenodd"/>', // Briefcase
  list: '<path d="M3.496 4.516A1.49 1.49 0 002 6c0 .82.67 1.485 1.496 1.485A1.49 1.49 0 004.99 6c0-.82-.67-1.484-1.495-1.484zM2 12c0-.82.67-1.485 1.496-1.485A1.49 1.49 0 014.99 12c0 .82-.67 1.484-1.495 1.484A1.49 1.49 0 012 12zm1.496 4.531A1.49 1.49 0 002 18.016c0 .82.67 1.484 1.496 1.484a1.49 1.49 0 001.495-1.484c0-.82-.67-1.485-1.495-1.485zM7 5h15v2H7V5zm0 6h15v2H7v-2zm0 6h15v2H7v-2z"/>', // ListBulleted
};

/** Иконка как строка разметки (для шаблонов). */
export const icon = (name, size = 24) => `<svg class="ic" viewBox="0 0 24 24" width="${size}" height="${size}" fill="currentColor" aria-hidden="true" focusable="false">${GLYPHS[name]}</svg>`;

/** Знак Альфа-Банка: «А» с подчёркиванием-фундаментом. */
export const logoSign = (cls = 'logo__sign') => `<svg class="${cls}" viewBox="0 0 18 24" fill="currentColor" aria-hidden="true" focusable="false"><path d="M9.103 2c-1.485 0-1.946.879-2.342 2l-4.08 11.557h2.706l.942-2.747h5.206l.873 2.747h2.877L11.397 4c-.376-1.117-.81-2-2.294-2zm-.071 3.01h-.069l-1.85 5.479h3.665L9.032 5.01zM15.593 22H2.407v-2.73h13.186V22z" fill-rule="evenodd" clip-rule="evenodd"/></svg>`;
