import tkinter as tk
from tkinter import messagebox, scrolledtext, ttk
import tkinter.font as tkfont
import urllib.request
import urllib.error
import urllib.parse
import json

class GedcomXBrowserApp:
    def __init__(self, root):
        self.root = root
        self.root.title("GEDCOM X RS Hypermedia Browser")
        self.root.geometry("1450x850")

        self.headers = {'Accept': 'application/x-gedcomx-v1+json'}
        self.server_url_var = tk.StringVar(value="http://localhost:8080")

        # State / History management for Active Resource (Right Pane)
        self.history_stack = []
        self.forward_stack = []
        self.current_url = None
        self.current_document = {}

        # State management for Collection List (Left Pane)
        self.loaded_entities = {}
        self._collection_urls = {}
        self._collection_level_links = {}
        self.next_page_url = None
        self.is_fetching_page = False
        self._ignore_tree_select = False

        self.visual_parents = []
        self.family_groups = []
        self.active_family_index = 0
        self.current_visual_person = None

        self.is_busy = False

        self.create_widgets()

    def create_widgets(self):
        self.create_menu()

        # --- In-App Notification Banner ---
        self.notification_frame = tk.Frame(self.root, bg="#e8f4f8", height=28)
        self.notification_frame.pack(fill=tk.X, padx=10, pady=(5, 0))
        self.notification_frame.pack_propagate(False)

        self.notification_label = tk.Label(
            self.notification_frame,
            text="Welcome! Choose File > New Connection... to discover server collections.",
            bg="#e8f4f8",
            fg="#1a5276",
            font=("Arial", 10, "bold"),
            anchor="w"
        )
        self.notification_label.pack(side=tk.LEFT, padx=10, fill=tk.BOTH, expand=True)

        # --- Main Layout (PanedWindow) ---
        main_pane = ttk.PanedWindow(self.root, orient=tk.HORIZONTAL)
        main_pane.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        # Left Pane: Collection Explorer
        left_frame = ttk.LabelFrame(main_pane, text="Entities in the Collection")
        main_pane.add(left_frame, weight=1)

        # Left Header: Collection & Browse Menus
        left_header = ttk.Frame(left_frame)
        left_header.pack(fill=tk.X, padx=5, pady=5)

        ttk.Label(left_header, text="Collection:").pack(side=tk.TOP, anchor=tk.W)
        self.collection_var = tk.StringVar()
        self.collection_combo = ttk.Combobox(left_header, textvariable=self.collection_var, state="readonly")
        self.collection_combo.pack(side=tk.TOP, fill=tk.X, pady=(0, 8))
        self.collection_combo.bind("<<ComboboxSelected>>", self.on_collection_selected)

        ttk.Label(left_header, text="Browse Collection Entities:").pack(side=tk.TOP, anchor=tk.W)
        self.coll_link_var = tk.StringVar()
        self.coll_link_combo = ttk.Combobox(left_header, textvariable=self.coll_link_var, state="readonly")
        self.coll_link_combo.pack(side=tk.TOP, fill=tk.X, pady=(0, 5))
        self.coll_link_combo.bind("<<ComboboxSelected>>", self.on_collection_link_selected)

        # Treeview Configuration
        tree_frame = ttk.Frame(left_frame)
        tree_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        self.entity_tree = ttk.Treeview(tree_frame, columns=("Type", "ID", "Name"), show="headings", selectmode="browse")
        self._entity_column_labels = {"Type": "Type", "ID": "ID", "Name": "Name / Title"}
        self._entity_sort_column = None
        self._entity_sort_reverse = False

        for column, label in self._entity_column_labels.items():
            self.entity_tree.heading(
                column,
                text=label,
                command=lambda c=column: self.sort_entity_tree(c),
            )
        self.entity_tree.column("Type", width=80, stretch=False)
        self.entity_tree.column("ID", width=100, stretch=False)
        self.entity_tree.column("Name", width=180)

        # Vertical Scrollbar bound to pagination listener
        self.tree_scroll = ttk.Scrollbar(tree_frame, orient="vertical", command=self.entity_tree.yview)
        self.entity_tree.configure(yscrollcommand=self.tree_scroll_set)

        self.entity_tree.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        self.tree_scroll.pack(side=tk.RIGHT, fill=tk.Y)

        self.entity_tree.bind("<<TreeviewSelect>>", self.on_entity_select)

        # Right Pane: Links & Data (Active State)
        right_frame = ttk.Frame(main_pane)
        main_pane.add(right_frame, weight=3)

        # State Transitions Container (Houses Nav buttons + Dynamic Links)
        self.links_frame = ttk.LabelFrame(right_frame, text="Available State Transitions (Active Resource)")
        self.links_frame.pack(fill=tk.X, padx=5, pady=5)

        # Embedded Navigation Buttons (Left side)
        nav_frame = ttk.Frame(self.links_frame)
        nav_frame.pack(side=tk.LEFT, padx=(5, 2), pady=5)

        self.back_btn = ttk.Button(nav_frame, text="⬅ Back", command=self.go_back, state=tk.DISABLED, width=8)
        self.back_btn.pack(side=tk.LEFT, padx=2)

        self.forward_btn = ttk.Button(nav_frame, text="Forward ➡", command=self.go_forward, state=tk.DISABLED, width=10)
        self.forward_btn.pack(side=tk.LEFT, padx=2)

        # Vertical Separator between Nav Buttons and Dynamic Transition Buttons
        sep = ttk.Separator(self.links_frame, orient="vertical")
        sep.pack(side=tk.LEFT, fill=tk.Y, padx=5, pady=2)

        # Inner container for dynamic HATEOAS state transition buttons
        self.links_inner_frame = ttk.Frame(self.links_frame)
        self.links_inner_frame.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=2, pady=5)

        self.details_notebook = ttk.Notebook(right_frame)
        self.details_notebook.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        self.visual_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.visual_tab, text="Family")

        self.person_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.person_tab, text="Person")
        person_tree_frame = ttk.Frame(self.person_tab)
        person_tree_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        self.person_detail_tree = ttk.Treeview(
            person_tree_frame, columns=("Field", "Value"), show="headings", selectmode="none"
        )
        self.person_detail_tree.heading("Field", text="Field")
        self.person_detail_tree.heading("Value", text="Value")
        self.person_detail_tree.column("Field", width=140, stretch=False, anchor=tk.W)
        self.person_detail_tree.column("Value", width=600, anchor=tk.W)
        person_tree_scroll = ttk.Scrollbar(person_tree_frame, orient="vertical", command=self.person_detail_tree.yview)
        self.person_detail_tree.configure(yscrollcommand=person_tree_scroll.set)
        self.person_detail_tree.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        person_tree_scroll.pack(side=tk.RIGHT, fill=tk.Y)

        self.ancestry_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.ancestry_tab, text="Ancestry")

        self.json_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.json_tab, text="Details (JSON)")
        self.json_text = scrolledtext.ScrolledText(self.json_tab, wrap=tk.WORD, font=("Consolas", 10))
        self.json_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        self.setup_visual_canvas()
        self.setup_ancestry_canvas()

        # The Ancestry canvas can't get accurate geometry until its tab has
        # actually been shown at least once (see _center_ancestry_view) --
        # re-center as soon as that happens.
        self.details_notebook.bind("<<NotebookTabChanged>>", self._on_notebook_tab_changed)

    def _on_notebook_tab_changed(self, event):
        try:
            selected = self.details_notebook.select()
        except tk.TclError:
            return
        if selected == str(self.ancestry_tab):
            self._center_ancestry_view()

    def create_menu(self):
        menu_bar = tk.Menu(self.root)
        file_menu = tk.Menu(menu_bar, tearoff=False)
        file_menu.add_command(label="New Connection...", command=self.show_connection_dialog)
        file_menu.add_separator()
        file_menu.add_command(label="Exit", command=self.root.destroy)
        menu_bar.add_cascade(label="File", menu=file_menu)

        help_menu = tk.Menu(menu_bar, tearoff=False)
        help_menu.add_command(label="About", command=self.show_about_dialog)
        menu_bar.add_cascade(label="Help", menu=help_menu)
        self.root.config(menu=menu_bar)

    def show_connection_dialog(self):
        dialog = tk.Toplevel(self.root)
        dialog.title("New Connection")
        dialog.resizable(False, False)
        dialog.transient(self.root)
        dialog.grab_set()

        content = ttk.Frame(dialog, padding=12)
        content.pack(fill=tk.BOTH, expand=True)
        ttk.Label(content, text="Server URL:").grid(row=0, column=0, sticky=tk.W, padx=(0, 8), pady=(0, 12))
        url_var = tk.StringVar(value=self.server_url_var.get())
        url_entry = ttk.Entry(content, textvariable=url_var, width=42)
        url_entry.grid(row=0, column=1, sticky=tk.EW, pady=(0, 12))
        content.columnconfigure(1, weight=1)

        def connect():
            server_url = url_var.get().strip()
            if not server_url:
                messagebox.showwarning("New Connection", "Please enter a server URL.", parent=dialog)
                return
            self.server_url_var.set(server_url)
            dialog.destroy()
            self.fetch_collections()

        button_frame = ttk.Frame(content)
        button_frame.grid(row=1, column=0, columnspan=2, sticky=tk.E)
        ttk.Button(button_frame, text="Cancel", command=dialog.destroy).pack(side=tk.RIGHT)
        ttk.Button(button_frame, text="Connect", command=connect).pack(side=tk.RIGHT, padx=(0, 8))
        dialog.bind("<Return>", lambda event: connect())
        dialog.bind("<Escape>", lambda event: dialog.destroy())
        url_entry.focus_set()
        url_entry.selection_range(0, tk.END)

    def show_about_dialog(self):
        messagebox.showinfo("About", "GEDCOM X RS Hypermedia Browser", parent=self.root)

    def setup_visual_canvas(self):
        self.visual_canvas = tk.Canvas(self.visual_tab, bg="#f5f7fa")
        self.visual_scrollbar_y = ttk.Scrollbar(self.visual_tab, orient="vertical", command=self.visual_canvas.yview)

        self.visual_scrollable_frame = ttk.Frame(self.visual_canvas)
        self.visual_scrollable_frame.bind(
            "<Configure>",
            lambda e: self.visual_canvas.configure(scrollregion=self.visual_canvas.bbox("all"))
        )
        self.canvas_window = self.visual_canvas.create_window((0, 0), window=self.visual_scrollable_frame, anchor="nw")
        self.visual_canvas.configure(yscrollcommand=self.visual_scrollbar_y.set)
        self.visual_canvas.bind('<Configure>', self._on_canvas_resize)

        self.visual_canvas.pack(side="left", fill="both", expand=True)
        self.visual_scrollbar_y.pack(side="right", fill="y")

    def _on_canvas_resize(self, event):
        canvas_width = event.width
        self.visual_canvas.itemconfig(self.canvas_window, width=canvas_width)

    def setup_ancestry_canvas(self):
        """A plain 2D-scrollable canvas for the pedigree tree -- unlike the
        Family tab, content here can be wider AND taller than the viewport
        (5 generations = up to 31 cards), so it gets both scrollbars rather
        than the single vertical one the auto-stretching Family canvas uses."""
        self.ancestry_canvas = tk.Canvas(self.ancestry_tab, bg="#f5f7fa")
        ancestry_vscroll = ttk.Scrollbar(self.ancestry_tab, orient="vertical", command=self.ancestry_canvas.yview)
        ancestry_hscroll = ttk.Scrollbar(self.ancestry_tab, orient="horizontal", command=self.ancestry_canvas.xview)
        self.ancestry_canvas.configure(yscrollcommand=ancestry_vscroll.set, xscrollcommand=ancestry_hscroll.set)

        self.ancestry_canvas.grid(row=0, column=0, sticky="nsew")
        ancestry_vscroll.grid(row=0, column=1, sticky="ns")
        ancestry_hscroll.grid(row=1, column=0, sticky="ew")
        self.ancestry_tab.rowconfigure(0, weight=1)
        self.ancestry_tab.columnconfigure(0, weight=1)

    def show_notification(self, message, level="info"):
        colors = {
            "info": ("#e8f4f8", "#1a5276"),      # Light Blue
            "success": ("#e8f8f5", "#117a65"),   # Mint Green
            "warning": ("#fef9e7", "#b7950b"),   # Light Yellow/Brown
            "error": ("#fadbd8", "#78281f")      # Light Red
        }
        bg_color, fg_color = colors.get(level, colors["info"])
        self.notification_frame.config(bg=bg_color)
        self.notification_label.config(text=message, bg=bg_color, fg=fg_color)

    # --- Discovery / Entry Point ---
    def fetch_collections(self):
        base_url = self.server_url_var.get().strip().rstrip('/')
        full_url = f"{base_url}/collections"

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    self.show_notification("Server returned 204 No Content for collections.", "warning")
                    return

                parsed_json = json.loads(response.read().decode('utf-8'))

                if 'collections' in parsed_json:
                    combo_values = []
                    self._collection_urls = {}
                    for col in parsed_json['collections']:
                        col_id = col.get('id', 'Unknown')
                        display_name = f"{col.get('title', 'Unnamed')} ({col_id})"

                        self_href = self._link_href(col.get('links', {}), 'self')
                        if not self_href:
                            self_href = f"/collections/{col_id}"

                        self._collection_urls[display_name] = self_href
                        combo_values.append(display_name)

                    self.collection_combo['values'] = combo_values
                    self.collection_combo.set("")
                    self.coll_link_combo.set("")
                    self.coll_link_combo['values'] = []

                    if combo_values:
                        self.show_notification(f"Connected! {len(combo_values)} collection(s) found. Select one from the menu.", "success")
        except Exception as e:
            self.show_notification(f"Could not connect to server: {e}", "error")

    def on_collection_selected(self, event):
        """Triggered when user selects a Collection from the left pane dropdown."""
        if self.is_busy:
            return
        selected = self.collection_var.get()
        target_href = self._collection_urls.get(selected)
        if not target_href:
            return

        # The previously displayed resource (JSON pane, Family tab, nav
        # history) belongs to the collection being left -- clear it before
        # loading the new one so stale details don't linger on screen.
        self.reset_active_resource_view()

        base_url = self.server_url_var.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", target_href)

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    self.show_notification("Server returned 204 No Content for selected collection.", "warning")
                    return
                doc = json.loads(response.read().decode('utf-8'))

                self._collection_level_links = doc.get('links', {})
                link_keys = list(self._collection_level_links.keys())
                self.coll_link_combo['values'] = link_keys

                if "persons" in link_keys:
                    self.coll_link_combo.set("persons")
                    self.on_collection_link_selected(None)
                elif link_keys:
                    self.coll_link_combo.set(link_keys[0])
                    self.on_collection_link_selected(None)

        except Exception as e:
            self.show_notification(f"Could not load collection details: {e}", "error")

    # Collection-level rels that point at a single Collection resource
    # (itself, or a parent/sibling collection) rather than a list of many
    # browsable items -- these don't fit the Left Pane list metaphor at all
    # (their document has a "collections" key, not persons/places/etc.), so
    # they're opened as the Active Resource instead of fed to the list.
    NON_LIST_COLLECTION_RELS = {"collection", "subcollections"}

    def on_collection_link_selected(self, event):
        """Triggered when a persistent Collection-level link is selected."""
        if self.is_busy:
            return
        rel = self.coll_link_var.get()
        href = self._link_href(self._collection_level_links, rel)
        if not href:
            return
        if rel in self.NON_LIST_COLLECTION_RELS:
            self.navigate_to(href, navigation_mode="new")
        else:
            self.load_collection_list(href, append=False)

    def load_collection_list(self, href, append=False):
        """Fetches entities for the Left Pane master list and detects pagination."""
        self.is_fetching_page = True
        base_url = self.server_url_var.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", href)

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    if not append:
                        for item in self.entity_tree.get_children():
                            self.entity_tree.delete(item)
                        self.loaded_entities.clear()
                    self.show_notification(f"No entries found at {href}.", "warning")
                    return

                doc = json.loads(response.read().decode('utf-8'))

                if not append:
                    for item in self.entity_tree.get_children():
                        self.entity_tree.delete(item)
                    self.loaded_entities.clear()

                before = len(self.entity_tree.get_children())
                self._append_entities_to_tree(doc)
                added = len(self.entity_tree.get_children()) - before
                self._apply_current_sort()

                self.next_page_url = self._link_href(doc.get('links', {}), "next")

                if added:
                    self.show_notification(f"Loaded {added} entr{'y' if added == 1 else 'ies'} from {href}.", "success")
                else:
                    # A 200 response that doesn't map to a shape this list
                    # knows how to render (persons/places/sourceDescriptions/
                    # relationships) -- surface that instead of doing nothing
                    # silently, which is what made this look broken.
                    self.show_notification(
                        f"{href} returned data, but not in a form this list can display.", "warning"
                    )

        except Exception as e:
            self.show_notification(f"Error loading collection list: {e}", "error")
        finally:
            self.is_fetching_page = False

    def tree_scroll_set(self, first, last):
        """Scroll listener for scroll-triggered loading (pagination)."""
        self.tree_scroll.set(first, last)
        if float(last) >= 0.99:
            self.root.after(50, self.load_next_page)

    def load_next_page(self):
        """Fired implicitly when scrolled to bottom."""
        if self.next_page_url and not self.is_fetching_page:
            self.show_notification("Loading next page of entities...", "info")
            self.load_collection_list(self.next_page_url, append=True)
            self.show_notification("Collection list loaded.", "success")

    # --- Hypermedia Navigation System (Right Pane Details) ---
    def navigate_to(self, href, navigation_mode="new"):
        if self.is_busy:
            # Already loading something -- ignore the extra click rather
            # than starting an overlapping request. It doesn't get lost;
            # once we're idle again the next click works normally.
            return

        base_url = self.server_url_var.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", href)

        if self.current_url:
            if navigation_mode == "new":
                self.history_stack.append(self.current_url)
                self.forward_stack.clear()
            elif navigation_mode == "back":
                self.forward_stack.append(self.current_url)
            elif navigation_mode == "forward":
                self.history_stack.append(self.current_url)

        self.current_url = full_url

        self.set_busy(True, f"Loading {href}…")
        self.json_text.delete(1.0, tk.END)
        self.json_text.insert(tk.END, f"GET {full_url}...\n\n")
        self.root.update()

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    self.show_notification(f"HTTP 204 No Content: No resource exists at '{href}'.", "warning")
                    return

                raw_data = response.read().decode('utf-8')
                if not raw_data.strip():
                    self.show_notification(f"HTTP 204 No Content: Empty response body received.", "warning")
                    return

                self.current_document = json.loads(raw_data)
                self.json_text.delete(1.0, tk.END)
                self.json_text.insert(tk.END, json.dumps(self.current_document, indent=4))

                self.render_link_buttons(self.current_document.get('links', {}), "Active Resource")

                main_person = None
                if self.current_document.get('persons'):
                    main_person = self.current_document['persons'][0]
                    self.draw_3_generation_view(main_person)
                    self.render_person_detail_tab(main_person)
                    self.render_ancestry_tab(main_person)
                else:
                    self.clear_visual_canvas()
                    self.render_empty_visual_state("No visual representation mapped for this resource.")
                    self.render_person_detail_tab(None)
                    self.ancestry_canvas.delete("all")

                if main_person:
                    self.highlight_entity_in_tree(main_person.get('id'))

                self.show_notification(f"State loaded successfully from {href}", "success")

        except urllib.error.HTTPError as e:
            if e.code == 204:
                self.show_notification(f"HTTP 204 No Content: The requested relationship or entity does not exist.", "warning")
            else:
                self.show_notification(f"HTTP Error {e.code}: {e.reason}", "error")
        except Exception as e:
            self.show_notification(f"Error navigating to {href}: {e}", "error")
        finally:
            self.set_busy(False)

    def go_back(self):
        if self.history_stack:
            prev_url = self.history_stack.pop()
            self.navigate_to(prev_url, navigation_mode="back")

    def go_forward(self):
        if self.forward_stack:
            next_url = self.forward_stack.pop()
            self.navigate_to(next_url, navigation_mode="forward")

    def update_ui_state(self):
        self.back_btn.config(state=tk.NORMAL if self.history_stack else tk.DISABLED)
        self.forward_btn.config(state=tk.NORMAL if self.forward_stack else tk.DISABLED)

    def set_busy(self, busy, message=None):
        """Puts up (or clears) the 'holding page' during a slow operation --
        the pagination search in highlight_entity_in_tree, in particular,
        can take several seconds against a large database. While busy: the
        cursor becomes a watch/hourglass, the entity list is visually
        disabled, and nav buttons are disabled. navigate_to() also checks
        is_busy and no-ops on re-entry, so repeated clicks queue up as no-ops
        instead of stacking overlapping requests."""
        self.is_busy = busy
        if busy:
            self.root.config(cursor="watch")
            self.entity_tree.state(['disabled'])
            self.back_btn.config(state=tk.DISABLED)
            self.forward_btn.config(state=tk.DISABLED)
            if message:
                self.show_notification(message, "info")
        else:
            self.root.config(cursor="")
            self.entity_tree.state(['!disabled'])
            self.update_ui_state()
        self.root.update()

    def reset_active_resource_view(self):
        """Clears the right-hand Active Resource pane (JSON tab, Family tab,
        state-transition buttons) and navigation history. Used when switching
        Collections, since whatever was previously displayed there belongs to
        the collection being left, not the one being entered."""
        self.history_stack.clear()
        self.forward_stack.clear()
        self.current_url = None
        self.current_document = {}
        self.update_ui_state()

        self.json_text.delete(1.0, tk.END)
        self.render_link_buttons({}, "Active Resource")

        self.visual_parents = []
        self.family_groups = []
        self.active_family_index = 0
        self.current_visual_person = None
        self.clear_visual_canvas()
        self.render_empty_visual_state("Select an entity from the list to view its details.")
        self.render_person_detail_tab(None)
        self._render_ancestry_empty_state("Select an entity from the list to view its details.")

    # --- Treeview Logic ---
    def sort_entity_tree(self, column):
        if column == self._entity_sort_column:
            self._entity_sort_reverse = not self._entity_sort_reverse
        else:
            self._entity_sort_column = column
            self._entity_sort_reverse = False

        self._execute_sort()

    def _apply_current_sort(self):
        if self._entity_sort_column:
            self._execute_sort()

    def _execute_sort(self):
        column = self._entity_sort_column
        column_index = self.entity_tree["columns"].index(column)
        rows = [
            (self._entity_sort_key(column, self.entity_tree.item(item, "values")[column_index]), item)
            for item in self.entity_tree.get_children("")
        ]
        rows.sort(key=lambda row: row[0], reverse=self._entity_sort_reverse)
        for position, (_, item) in enumerate(rows):
            self.entity_tree.move(item, "", position)

        direction = " ▼" if self._entity_sort_reverse else " ▲"
        for name, label in self._entity_column_labels.items():
            suffix = direction if name == column else ""
            self.entity_tree.heading(name, text=label + suffix)

    @staticmethod
    def _entity_sort_key(column, value):
        text = str(value).casefold()
        if column == "ID":
            prefix = text.rstrip("0123456789")
            number = text[len(prefix):]
            if number:
                return prefix, int(number)
        return text,

    def _append_entities_to_tree(self, doc):
        for person in doc.get('persons', []):
            pid = person.get('id', 'Unknown')
            name = person.get('display', {}).get('name', 'Unknown Name')
            if name == 'Unknown Name':
                try: name = person['names'][0]['nameForms'][0]['fullText']
                except: pass
            tree_id = self.entity_tree.insert("", tk.END, values=("Person", pid, name))
            self.loaded_entities[tree_id] = ("Person", person)

        for place in doc.get('places', []):
            pid = place.get('id', 'Unknown')
            name = "Unknown Place"
            try: name = place['names'][0]['value']
            except: pass
            tree_id = self.entity_tree.insert("", tk.END, values=("Place", pid, name))
            self.loaded_entities[tree_id] = ("Place", place)

        for src in doc.get('sourceDescriptions', []):
            sid = src.get('id', 'Unknown')
            title = "Unknown Source"
            try: title = src['titles'][0]['value']
            except: pass
            tree_id = self.entity_tree.insert("", tk.END, values=("Source", sid, title))
            self.loaded_entities[tree_id] = ("Source", src)

        for rel in doc.get('relationships', []):
            rid = rel.get('id', 'Unknown')
            rel_type = rel.get('type', '')
            type_label = rel_type.rsplit('/', 1)[-1] if rel_type else 'Relationship'
            p1 = rel.get('person1', {}).get('resourceId', '?')
            p2 = rel.get('person2', {}).get('resourceId', '?')
            arrow = '→' if type_label == 'ParentChild' else '+'
            summary = f"{type_label}: {p1} {arrow} {p2}"
            tree_id = self.entity_tree.insert("", tk.END, values=("Relationship", rid, summary))
            self.loaded_entities[tree_id] = ("Relationship", rel)

    def highlight_entity_in_tree(self, entity_id):
        """Visually selects an entity in the list without triggering navigation."""
        if not entity_id:
            return
        if self._select_tree_item_if_present(entity_id):
            return
        # Not loaded yet -- the left pane only holds whatever pages have been
        # scrolled into view. Keep pulling pages until we find it or run out.
        # Against a large database this can mean fetching many pages in a
        # row (e.g. reaching a person a couple of thousand IDs deep), which
        # is the slow part of navigating -- keep the holding message and the
        # window repainted so it's clear something is still happening rather
        # than the app looking frozen.
        pages_checked = 0
        while self.next_page_url and not self.is_fetching_page:
            pages_checked += 1
            self.show_notification(
                f"Still looking for {entity_id} in the Persons list (checked {pages_checked} more page"
                f"{'s' if pages_checked != 1 else ''})…", "info"
            )
            self.root.update()
            self.load_collection_list(self.next_page_url, append=True)
            if self._select_tree_item_if_present(entity_id):
                return

    def _select_tree_item_if_present(self, entity_id):
        for item in self.entity_tree.get_children():
            values = self.entity_tree.item(item)['values']
            if len(values) >= 2 and str(values[1]) == str(entity_id):
                self._ignore_tree_select = True
                self.entity_tree.selection_set(item)
                self.entity_tree.see(item)
                self.root.update()
                self._ignore_tree_select = False
                return True
        return False

    def on_entity_select(self, event):
        """User clicked an entity in the master list - fetch and show its state."""
        if self._ignore_tree_select or self.is_busy:
            return

        selection = self.entity_tree.selection()
        if not selection:
            return

        tree_id = selection[0]
        entity_tuple = self.loaded_entities.get(tree_id)

        if entity_tuple:
            entity_type, entity_data = entity_tuple
            links = entity_data.get('links', {})
            href = None

            # Map entity types to their standard GEDCOM X link relations
            target_relations = []
            if entity_type == "Person":
                target_relations = ["person", "self"]
            elif entity_type == "Place":
                target_relations = ["place", "description", "self"]
            elif entity_type == "Source":
                target_relations = ["description", "source", "self"]
            elif entity_type == "Relationship":
                target_relations = ["relationship", "self"]

            # Iterate through the preferred hypermedia relations first
            for rel in target_relations:
                href = self._link_href(links, rel)
                if href:
                    break

            # Fallback for systems that use the 'about' URI for Descriptions
            if not href and 'about' in entity_data:
                href = entity_data['about']

            if href:
                self.navigate_to(href, navigation_mode="new")
            else:
                self.show_notification(f"No valid hypermedia link found for this {entity_type}.", "warning")

    # --- Utilities / Buttons / Visuals ---
    def render_link_buttons(self, links_dict, context_label):
        self.links_frame.config(text=f"Available State Transitions ({context_label})")
        for widget in self.links_inner_frame.winfo_children():
            widget.destroy()

        if not links_dict:
            ttk.Label(self.links_inner_frame, text="No links available.", font=("", 10, "italic")).pack(side=tk.LEFT)
            return

        for rel, link_data in links_dict.items():
            if isinstance(link_data, dict): href = link_data.get('href')
            elif isinstance(link_data, list) and len(link_data) > 0: href = link_data[0].get('href')
            else: continue

            if href:
                btn = ttk.Button(self.links_inner_frame, text=rel, command=lambda h=href: self.navigate_to(h, navigation_mode="new"))
                btn.pack(side=tk.LEFT, padx=2, pady=2)

    def clear_visual_canvas(self):
        for widget in self.visual_scrollable_frame.winfo_children():
            widget.destroy()

    def render_empty_visual_state(self, message):
        center_container = ttk.Frame(self.visual_scrollable_frame)
        center_container.pack(expand=True, fill=tk.BOTH, pady=100)
        ttk.Label(center_container, text=message, font=("Arial", 12, "italic")).pack(anchor=tk.CENTER)

    def create_person_card(self, parent_widget, person_data, is_selected=False):
        border_color = "#2b5c8f" if is_selected else "#bdc3c7"
        bg_color = "#ebf5fb" if is_selected else "#ffffff"
        border_width = 3 if is_selected else 1

        card = tk.Frame(
            parent_widget,
            bg=bg_color,
            highlightbackground=border_color,
            highlightthickness=border_width,
            width=260,
            height=145,
            padx=12,
            pady=10,
            cursor="hand2"
        )
        card.pack_propagate(False)

        display = person_data.get('display', {})
        name = display.get('name', 'Unknown Name')
        gender = display.get('gender', 'Unknown Gender')
        lifespan = display.get('lifespan', '')

        if name == 'Unknown Name':
            try: name = person_data['names'][0]['nameForms'][0]['fullText']
            except: pass

        if gender == 'Unknown Gender':
            try: gender = person_data['gender']['type'].split('/')[-1]
            except: pass

        role_text = "Active Person" if is_selected else "Person"
        lbl_role = tk.Label(card, text=role_text.upper(), font=("Arial", 8, "bold"), fg="#2b5c8f" if is_selected else "gray", bg=bg_color)
        lbl_role.pack(anchor=tk.W)

        lbl_name = tk.Label(card, text=name, font=("Arial", 12, "bold"), bg=bg_color, wraplength=230, justify=tk.LEFT)
        lbl_name.pack(anchor=tk.W, pady=(2, 0))

        clickable_widgets = [card, lbl_role, lbl_name]
        if lifespan:
            lbl_life = tk.Label(card, text=lifespan, font=("Arial", 10), fg="#16a085", bg=bg_color)
            lbl_life.pack(anchor=tk.W)
            clickable_widgets.append(lbl_life)

        lbl_gen = tk.Label(card, text=f"Gender: {gender}", font=("Arial", 9), bg=bg_color)
        lbl_gen.pack(anchor=tk.W, pady=(4, 0))

        pid = person_data.get('id', 'N/A')
        lbl_id = tk.Label(card, text=f"ID: {pid}", font=("Arial", 8), fg="gray", bg=bg_color)
        lbl_id.pack(anchor=tk.W)

        clickable_widgets.extend((lbl_gen, lbl_id))
        if is_selected:
            # This card already IS the active resource -- clicking it again
            # would just re-fetch and re-display the exact same thing (the
            # pointless reload loop). Jump to the Person tab instead.
            click_handler = lambda e, person=person_data: self.show_person_detail_tab(person)
        else:
            click_handler = lambda e, person=person_data: self.open_person_card(person)
        for widget in clickable_widgets:
            widget.bind("<Button-1>", click_handler)

        return card

    def open_person_card(self, person_data):
        href = self._link_href(person_data.get("links", {}), "person")
        if not href:
            href = self._link_href(person_data.get("links", {}), "self")

        if href:
            self.navigate_to(href, navigation_mode="new")
        else:
            self.show_notification(f"Could not open person: no person/self link was supplied.", "warning")

    def show_person_detail_tab(self, person_data):
        """Renders person_data into the Person tab and switches to it. Used
        when the user clicks the already-active person's own card, where
        re-navigating via open_person_card would just re-fetch the resource
        that's already on screen."""
        self.render_person_detail_tab(person_data)
        self.details_notebook.select(self.person_tab)

    def render_person_detail_tab(self, person_data):
        """Populates the Person tab's Field/Value table from a GEDCOM X
        Person document. Pass None to clear it."""
        for item in self.person_detail_tree.get_children():
            self.person_detail_tree.delete(item)

        if not person_data:
            return

        def add_row(field, value):
            if value:
                self.person_detail_tree.insert("", tk.END, values=(field, value))

        display = person_data.get('display', {})

        name = display.get('name')
        if not name:
            try: name = person_data['names'][0]['nameForms'][0]['fullText']
            except Exception: name = None
        add_row("Name", name)

        for n in person_data.get('names', []):
            try: full_text = n['nameForms'][0]['fullText']
            except Exception: full_text = None
            if full_text and full_text != name:
                label = f"{n['type'].split('/')[-1]} name" if n.get('type') else "Alternate name"
                add_row(label, full_text)

        gender = display.get('gender')
        if not gender:
            try: gender = person_data['gender']['type'].split('/')[-1]
            except Exception: gender = None
        add_row("Gender", gender)

        if person_data.get('living') is not None:
            add_row("Living", "Yes" if person_data['living'] else "No")

        add_row("Lifespan", display.get('lifespan'))
        add_row("ID", person_data.get('id'))

        for fact in person_data.get('facts', []):
            fact_type = fact.get('type', '')
            label = fact_type.split('/')[-1] if fact_type else "Fact"
            parts = []
            date = (fact.get('date') or {}).get('original')
            place = (fact.get('place') or {}).get('original')
            value = fact.get('value')
            if date: parts.append(date)
            if place: parts.append(place)
            if value: parts.append(value)
            add_row(label, " — ".join(parts) if parts else "(no detail recorded)")

        for note in person_data.get('notes', []):
            add_row("Note", note.get('text'))

        sources = person_data.get('sources', [])
        if sources:
            add_row("Sources", f"{len(sources)} attached")

    def _link_href(self, links, relation):
        link = links.get(relation)
        if isinstance(link, dict):
            return link.get("href")
        if isinstance(link, list) and link and isinstance(link[0], dict):
            return link[0].get("href")
        return None

    def fetch_relatives(self, person_data, relation):
        """Fetch related persons for a person, paired with their relationship
        object (may be None if the server omitted it)."""
        href = self._link_href(person_data.get("links", {}), relation)
        if not href:
            return []

        base_url = self.server_url_var.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", href)
        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    return []
                raw_data = response.read().decode("utf-8")

            if not raw_data.strip():
                return []
            document = json.loads(raw_data)
            persons = document.get("persons", [])
            relationships = document.get("relationships", [])
            return [(p, relationships[i] if i < len(relationships) else None)
                    for i, p in enumerate(persons)]
        except Exception as e:
            self.show_notification(f"Could not load {relation} for this person: {e}", "warning")
        return []

    def draw_3_generation_view(self, selected_person):
        self.current_visual_person = selected_person
        self.visual_parents = [p for p, _ in self.fetch_relatives(selected_person, "parents")]
        self.family_groups = self.build_family_groups(selected_person)
        self.active_family_index = 0
        self.render_visual_tab()

    def build_family_groups(self, selected_person):
        """Group this person's children under the spouse they belong to,
        using the family id embedded in each relationship's id (e.g. a couple
        relationship 'F7' and a parent-child relationship 'F7-FC12' share the
        'F7' family id)."""
        spouses = self.fetch_relatives(selected_person, "spouses")
        children = self.fetch_relatives(selected_person, "children")

        groups = []
        group_by_family_key = {}
        for spouse, rel in spouses:
            family_key = rel.get('id') if rel else None
            group = {"spouse": spouse, "children": []}
            groups.append(group)
            if family_key:
                group_by_family_key[family_key] = group

        other_group = None
        for child, rel in children:
            family_key = rel['id'].split('-')[0] if rel and rel.get('id') else None
            group = group_by_family_key.get(family_key)
            if group is None:
                if other_group is None:
                    other_group = {"spouse": None, "children": []}
                group = other_group
            group["children"].append(child)

        if other_group is not None:
            groups.append(other_group)
        if not groups:
            groups.append({"spouse": None, "children": []})

        return groups

    def family_switcher_label(self, group):
        count = len(group["children"])
        spouse = group["spouse"]
        if not spouse:
            return f"No listed spouse ({count})"
        name = spouse.get('display', {}).get('name', 'Unknown Name')
        if name == 'Unknown Name':
            try: name = spouse['names'][0]['nameForms'][0]['fullText']
            except Exception: pass
        return f"⚭ {name} ({count})"

    def on_family_switch(self):
        self.active_family_index = self.family_switch_var.get()
        self.render_visual_tab()

    def render_visual_tab(self):
        """Redraws the visual tab from already-fetched state (self.visual_parents,
        self.family_groups, self.active_family_index) -- no network calls, so
        switching the family tab is instant."""
        self.clear_visual_canvas()
        selected_person = self.current_visual_person

        outer_container = ttk.Frame(self.visual_scrollable_frame)
        outer_container.pack(expand=True, fill=tk.BOTH, padx=20, pady=20)

        # Generation 1: PARENTS
        gen1_frame = ttk.LabelFrame(outer_container, text="Parents", height=190)
        gen1_frame.pack(fill=tk.X, pady=(0, 10))
        gen1_frame.pack_propagate(False)

        parents_inner = ttk.Frame(gen1_frame)
        parents_inner.pack(anchor=tk.CENTER, expand=True)

        if self.visual_parents:
            for p in self.visual_parents:
                card = self.create_person_card(parents_inner, p, is_selected=False)
                card.pack(side=tk.LEFT, padx=10, pady=5)
        else:
            ttk.Label(parents_inner, text="No known parents for this person.", font=("Arial", 9, "italic")).pack(expand=True)

        # Family switcher -- only needed when there's more than one family to choose from
        if len(self.family_groups) > 1:
            switcher_frame = ttk.Frame(outer_container)
            switcher_frame.pack(pady=(0, 5))
            self.family_switch_var = tk.IntVar(value=self.active_family_index)
            for idx, group in enumerate(self.family_groups):
                rb = ttk.Radiobutton(
                    switcher_frame, text=self.family_switcher_label(group),
                    variable=self.family_switch_var, value=idx,
                    command=self.on_family_switch #, style="Toolbutton"
                )
                rb.pack(side=tk.LEFT, padx=4)

        active_group = self.family_groups[self.active_family_index]

        # Generation 2: SELECTED PERSON + SPOUSE
        gen2_frame = ttk.Frame(outer_container)
        gen2_frame.pack(fill=tk.X, pady=5)

        couple_inner = ttk.Frame(gen2_frame)
        couple_inner.pack(anchor=tk.CENTER)

        main_card = self.create_person_card(couple_inner, selected_person, is_selected=True)
        main_card.pack(side=tk.LEFT, padx=10, pady=5)

        if active_group["spouse"]:
            ttk.Label(couple_inner, text="⚭", font=("Arial", 14)).pack(side=tk.LEFT, padx=4)
            spouse_card = self.create_person_card(couple_inner, active_group["spouse"], is_selected=False)
            spouse_card.pack(side=tk.LEFT, padx=10, pady=5)

        # Generation 3: CHILDREN of the active family only
        children = active_group["children"]
        gen3_frame = ttk.LabelFrame(outer_container, text=f"Children ({len(children)})", height=210)
        gen3_frame.pack(fill=tk.X, pady=(10, 0))
        gen3_frame.pack_propagate(False)

        if children:
            child_canvas = tk.Canvas(gen3_frame, height=160, bg="#f5f7fa", highlightthickness=0)
            child_hscrollbar = ttk.Scrollbar(gen3_frame, orient="horizontal", command=child_canvas.xview)

            child_inner_frame = ttk.Frame(child_canvas)
            child_inner_frame.bind(
                "<Configure>",
                lambda e: child_canvas.configure(scrollregion=child_canvas.bbox("all"))
            )

            child_canvas.create_window((0, 0), window=child_inner_frame, anchor="nw")
            child_canvas.configure(xscrollcommand=child_hscrollbar.set)

            child_canvas.pack(fill=tk.X, expand=True)
            child_hscrollbar.pack(fill=tk.X)

            for c in children:
                card = self.create_person_card(child_inner_frame, c, is_selected=False)
                card.pack(side=tk.LEFT, padx=10, pady=5)
        else:
            children_inner = ttk.Frame(gen3_frame)
            children_inner.pack(anchor=tk.CENTER, expand=True)
            ttk.Label(children_inner, text="No known children for this person.", font=("Arial", 9, "italic")).pack(expand=True)

    # --- Ancestry tab: a left-to-right pedigree tree ---
    ANCESTRY_MAX_GENERATIONS = 5
    ANCESTRY_CARD_WIDTH = 200
    ANCESTRY_CARD_HEIGHT = 72
    ANCESTRY_COMPACT_CARD_HEIGHT = ANCESTRY_CARD_HEIGHT // 3
    ANCESTRY_COMPACT_FROM_GENERATION = 4  # generations 4+ get the slimmed-down card
    ANCESTRY_VERTICAL_GAP = 20
    ANCESTRY_GENERATION_OVERLAP = 1 / 3  # fraction of a card width shared with the previous generation
    ANCESTRY_CROSS_GENERATION_GAP = 4  # min clearance between a card and its parent/child in the next column

    def _ancestry_card_height(self, generation):
        if generation >= self.ANCESTRY_COMPACT_FROM_GENERATION:
            return self.ANCESTRY_COMPACT_CARD_HEIGHT
        return self.ANCESTRY_CARD_HEIGHT

    def render_ancestry_tab(self, selected_person):
        """Fetches the ancestry tree in a single request (the server already
        tags each person with their Ahnentafel number via
        display.ascendancyNumber) and lays it out as a pedigree fan: each
        generation is offset right (see ANCESTRY_GENERATION_OVERLAP), and
        vertically each generation's spacing halves as the generation count
        doubles, which keeps the whole tree centred on the root person for
        free."""
        self.ancestry_canvas.delete("all")

        href = self._link_href(selected_person.get("links", {}), "ancestry")
        if not href:
            self._render_ancestry_empty_state("No ancestry data available for this resource.")
            return

        separator = '&' if '?' in href else '?'
        href_with_generations = f"{href}{separator}generations={self.ANCESTRY_MAX_GENERATIONS}"
        base_url = self.server_url_var.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", href_with_generations)

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    persons = []
                else:
                    raw_data = response.read().decode("utf-8")
                    persons = json.loads(raw_data).get("persons", []) if raw_data.strip() else []
        except Exception as e:
            self.show_notification(f"Could not load ancestry data: {e}", "warning")
            self._render_ancestry_empty_state("Could not load ancestry data for this resource.")
            return

        if not persons:
            self._render_ancestry_empty_state("No ancestry data available for this resource.")
            return

        card_w = self.ANCESTRY_CARD_WIDTH
        # base_step must satisfy two different kinds of minimum spacing:
        #
        # 1. Same-generation: cards within one generation share an x
        #    position, so adjacent ones (gap = base_step / 2**(gen-2)) must
        #    clear their own card height + ANCESTRY_VERTICAL_GAP.
        #
        # 2. Cross-generation: because adjacent generations' columns overlap
        #    horizontally by design (ANCESTRY_GENERATION_OVERLAP), a card's
        #    x-range always overlaps its own parent/child's x-range too --
        #    so avoiding a visual collision with THEM depends entirely on
        #    the vertical offset between them (step = base_step /
        #    2**gen) clearing half of each card's height, plus
        #    ANCESTRY_CROSS_GENERATION_GAP. Skipping this check is what let
        #    generation 4/5 cards visually touch their own parents even
        #    though same-generation siblings had plenty of room.
        base_step = 0
        for gen in range(2, self.ANCESTRY_MAX_GENERATIONS + 1):
            required = (self._ancestry_card_height(gen) + self.ANCESTRY_VERTICAL_GAP) * (2 ** (gen - 2))
            base_step = max(base_step, required)
        for gen in range(1, self.ANCESTRY_MAX_GENERATIONS):
            combined_half_heights = (self._ancestry_card_height(gen) + self._ancestry_card_height(gen + 1)) / 2
            required = (2 ** gen) * (combined_half_heights + self.ANCESTRY_CROSS_GENERATION_GAP)
            base_step = max(base_step, required)

        positions = {}
        for person in persons:
            number_str = person.get('display', {}).get('ascendancyNumber')
            try:
                n = int(number_str)
            except (TypeError, ValueError):
                continue
            generation = n.bit_length()
            x, y = self._ancestry_position(n, card_w, base_step)
            positions[n] = (x, y, generation, person)

        # Connector lines first, so the cards drawn afterwards sit on top.
        # Both ends use each card's centre (card height varies by generation
        # now, so an edge-based anchor would no longer line up).
        for n, (x, y, _gen, _person) in positions.items():
            if n == 1:
                continue
            descendant_n = n // 2  # the person one generation closer to the root
            if descendant_n in positions:
                dx, dy, _dgen, _dperson = positions[descendant_n]
                self.ancestry_canvas.create_line(
                    x + card_w / 2, y, dx + card_w / 2, dy, fill="#b0b7c3", width=1
                )

        for n, (x, y, generation, person) in positions.items():
            card_h = self._ancestry_card_height(generation)
            compact = generation >= self.ANCESTRY_COMPACT_FROM_GENERATION
            self._create_ancestry_card(x, y, card_w, card_h, person, is_root=(n == 1), compact=compact)

        self.ancestry_canvas.configure(scrollregion=self.ancestry_canvas.bbox("all"))
        self._center_ancestry_view()

    def _ancestry_position(self, n, card_w, base_step):
        """Returns the (x, y) of the left-center point for Ahnentafel number
        n, relative to the root at (0, 0). Reads n's binary digits after the
        leading 1 as a path of father(0)/mother(1) choices from the root --
        each step halves the vertical offset and moves one generation-step
        to the right, which is the whole layout in one small loop."""
        generation_step = card_w * (1 - self.ANCESTRY_GENERATION_OVERLAP)
        bits = bin(n)[3:]
        generation = 1
        y = 0.0
        for bit in bits:
            generation += 1
            step = base_step / (2 ** (generation - 1))
            y += step if bit == '1' else -step
        x = (generation - 1) * generation_step
        return x, y

    def _truncate_text_to_width(self, text, font_spec, max_width):
        """Shortens text to fit max_width pixels at font_spec, appending an
        ellipsis if it had to cut anything. Used to guarantee a label never
        needs more than one line, since wraplength alone doesn't cap how
        many lines it wraps to."""
        font = tkfont.Font(font=font_spec)
        if font.measure(text) <= max_width:
            return text
        ellipsis = "…"
        lo, hi = 0, len(text)
        while lo < hi:
            mid = (lo + hi + 1) // 2
            if font.measure(text[:mid] + ellipsis) <= max_width:
                lo = mid
            else:
                hi = mid - 1
        return (text[:lo] + ellipsis) if lo > 0 else ellipsis

    def _create_ancestry_card(self, x, y, card_w, card_h, person_data, is_root=False, compact=False):
        border_color = "#2b5c8f" if is_root else "#bdc3c7"
        bg_color = "#ebf5fb" if is_root else "#ffffff"
        border_width = 3 if is_root else 1

        card = tk.Frame(
            self.ancestry_canvas, bg=bg_color,
            highlightbackground=border_color, highlightthickness=border_width,
            width=card_w, height=card_h, cursor="hand2"
        )
        card.pack_propagate(False)

        display = person_data.get('display', {})
        name = display.get('name', 'Unknown Name')
        if name == 'Unknown Name':
            try: name = person_data['names'][0]['nameForms'][0]['fullText']
            except Exception: pass

        if compact:
            # Generations 4+ : name only, no room for a second line. No
            # wraplength here deliberately -- a wrapped 2-3 line name won't
            # fit a 24px frame, and pack_propagate(False) only pins the
            # frame's own size, not the label's rendered text, so an
            # overflowing label can bleed into the row above/below. Truncate
            # to one line instead, so the content physically cannot exceed
            # what the card was sized for.
            compact_font = ("Arial", 8, "bold")
            display_name = self._truncate_text_to_width(name, compact_font, card_w - 10)
            lbl_name = tk.Label(
                card, text=display_name, font=compact_font, bg=bg_color, anchor="w"
            )
            lbl_name.pack(fill=tk.BOTH, expand=True, padx=5, pady=1)
            clickable = [card, lbl_name]
        else:
            lifespan = display.get('lifespan', '')
            lbl_name = tk.Label(
                card, text=name, font=("Arial", 9, "bold"), bg=bg_color,
                wraplength=card_w - 14, justify=tk.LEFT, anchor="w"
            )
            lbl_name.pack(fill=tk.X, padx=7, pady=(7, 0))
            clickable = [card, lbl_name]
            if lifespan:
                lbl_life = tk.Label(card, text=lifespan, font=("Arial", 8), fg="#16a085", bg=bg_color, anchor="w")
                lbl_life.pack(fill=tk.X, padx=7)
                clickable.append(lbl_life)

        for widget in clickable:
            widget.bind(
                "<Button-1>",
                lambda e, person=person_data, root=is_root: self._on_ancestry_card_click(person, root)
            )

        self.ancestry_canvas.create_window(x, y, window=card, anchor="w")

    def _on_ancestry_card_click(self, person_data, is_root):
        if is_root:
            # Same reasoning as the Family tab's active-person card: this
            # person IS the resource already on screen, so jump to the
            # Person tab instead of re-fetching and reloading it.
            self.show_person_detail_tab(person_data)
        else:
            self.open_person_card(person_data)

    def _render_ancestry_empty_state(self, message):
        self.ancestry_canvas.delete("all")
        self.ancestry_canvas.create_text(20, 20, anchor="nw", text=message, font=("Arial", 10, "italic"))
        self.ancestry_canvas.configure(scrollregion=self.ancestry_canvas.bbox("all"))

    def _center_ancestry_view(self):
        """Centers the tree in both directions when it's smaller than the
        viewport (the usual case), or anchors sensibly when it isn't: left
        (root visible) horizontally, centered on the root's y vertically.

        Centering works by padding the scrollregion itself out to the
        viewport size with the tree placed symmetrically inside it, then
        scrolling to the top-left of that padded region -- Tk has no direct
        "center small content in a bigger viewport" operation, so this
        fakes it by making the scrollable area exactly as big as what's
        visible, with the content in the middle of it.

        The viewport size is read from the Notebook, not the canvas itself:
        if the Ancestry tab isn't the selected one when this runs (e.g. the
        app just started on the Family tab), ttk.Notebook hasn't given the
        canvas real geometry yet and winfo_height()/width() on it would
        return a bogus tiny value -- which is why this used to render
        pinned to the top-left. The Notebook is always mapped regardless of
        which tab is showing, so it's a reliable stand-in. This is also
        re-run whenever the Ancestry tab becomes the selected one (see the
        <<NotebookTabChanged>> binding in create_widgets), so it corrects
        itself once real geometry is available."""
        self.ancestry_canvas.update_idletasks()
        bbox = self.ancestry_canvas.bbox("all")
        if not bbox:
            return
        x0, y0, x1, y1 = bbox
        content_w = x1 - x0
        content_h = y1 - y0
        if content_w <= 0 or content_h <= 0:
            return

        viewport_w = self.details_notebook.winfo_width()
        viewport_h = self.details_notebook.winfo_height()
        if viewport_w <= 1 or viewport_h <= 1:
            viewport_w = self.ancestry_canvas.winfo_width()
            viewport_h = self.ancestry_canvas.winfo_height()

        pad_w = max(0.0, (viewport_w - content_w) / 2)
        pad_h = max(0.0, (viewport_h - content_h) / 2)
        self.ancestry_canvas.configure(scrollregion=(x0 - pad_w, y0 - pad_h, x1 + pad_w, y1 + pad_h))

        # Horizontally: padding (if any) already centers the tree when
        # viewed from the region's left edge; when the tree is wider than
        # the viewport there's no padding, and starting from the left is
        # exactly "root visible first", which is still what we want.
        self.ancestry_canvas.xview_moveto(0.0)

        if pad_h > 0:
            self.ancestry_canvas.yview_moveto(0.0)
        else:
            target_top = 0 - viewport_h / 2  # root's y is 0; center it in the viewport
            fraction = (target_top - y0) / content_h
            self.ancestry_canvas.yview_moveto(max(0.0, min(1.0, fraction)))

if __name__ == "__main__":
    root = tk.Tk()
    app = GedcomXBrowserApp(root)
    root.mainloop()
