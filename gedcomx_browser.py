import tkinter as tk
from tkinter import messagebox, scrolledtext, ttk
import urllib.request
import urllib.error
import urllib.parse
import json

class GedcomXBrowserApp:
    def __init__(self, root):
        self.root = root
        self.root.title("GEDCOM X RS Hypermedia Browser")
        self.root.geometry("1150x850")

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

        # --- Main Toolbar (Navigation) ---
        toolbar_frame = ttk.Frame(self.root)
        toolbar_frame.pack(fill=tk.X, padx=10, pady=5)

        self.back_btn = ttk.Button(toolbar_frame, text="⬅ Back", command=self.go_back, state=tk.DISABLED)
        self.back_btn.pack(side=tk.LEFT, padx=2)

        self.forward_btn = ttk.Button(toolbar_frame, text="Forward ➡", command=self.go_forward, state=tk.DISABLED)
        self.forward_btn.pack(side=tk.LEFT, padx=2)

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

        self.links_frame = ttk.LabelFrame(right_frame, text="Available State Transitions (Links)")
        self.links_frame.pack(fill=tk.X, padx=5, pady=5)

        self.links_inner_frame = ttk.Frame(self.links_frame)
        self.links_inner_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        self.details_notebook = ttk.Notebook(right_frame)
        self.details_notebook.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        self.visual_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.visual_tab, text="Visual Representation")

        self.json_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.json_tab, text="Details (JSON)")
        self.json_text = scrolledtext.ScrolledText(self.json_tab, wrap=tk.WORD, font=("Consolas", 10))
        self.json_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        self.setup_visual_canvas()

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
                    # Menu explicitly defaults to blank
                    self.collection_combo.set("")
                    self.coll_link_combo.set("")
                    self.coll_link_combo['values'] = []

                    if combo_values:
                        self.show_notification(f"Connected! {len(combo_values)} collection(s) found. Select one from the menu.", "success")
        except Exception as e:
            self.show_notification(f"Could not connect to server: {e}", "error")

    def on_collection_selected(self, event):
        """Triggered when user selects a Collection from the left pane dropdown."""
        selected = self.collection_var.get()
        target_href = self._collection_urls.get(selected)
        if not target_href:
            return

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

    def on_collection_link_selected(self, event):
        """Triggered when a persistent Collection-level link is selected."""
        rel = self.coll_link_var.get()
        href = self._link_href(self._collection_level_links, rel)
        if href:
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
                    self.is_fetching_page = False
                    return

                doc = json.loads(response.read().decode('utf-8'))

                if not append:
                    for item in self.entity_tree.get_children():
                        self.entity_tree.delete(item)
                    self.loaded_entities.clear()

                self._append_entities_to_tree(doc)
                self._apply_current_sort()

                self.next_page_url = self._link_href(doc.get('links', {}), "next")

        except Exception as e:
            self.show_notification(f"Error loading collection list: {e}", "error")
        finally:
            self.is_fetching_page = False

    def tree_scroll_set(self, first, last):
        """Scroll listener for scroll-triggered loading (pagination)."""
        self.tree_scroll.set(first, last)
        # last == 1.0 indicates scrollbar is at the bottom bounds
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
        self.update_ui_state()

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

                # Render visual and highlight context
                main_person = None
                if self.current_document.get('persons'):
                    main_person = self.current_document['persons'][0]
                    self.draw_3_generation_view(main_person)
                else:
                    self.clear_visual_canvas()
                    self.render_empty_visual_state("No visual representation mapped for this resource.")

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

    def highlight_entity_in_tree(self, entity_id):
        """Visually selects an entity in the list without triggering navigation."""
        if not entity_id: return
        for item in self.entity_tree.get_children():
            values = self.entity_tree.item(item)['values']
            if len(values) >= 2 and str(values[1]) == str(entity_id):
                self._ignore_tree_select = True
                self.entity_tree.selection_set(item)
                self.entity_tree.see(item)
                self.root.update()
                self._ignore_tree_select = False
                return

    def on_entity_select(self, event):
        """User clicked an entity in the master list - fetch and show its state."""
        if self._ignore_tree_select:
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

            # 1. Iterate through the preferred hypermedia relations first
            for rel in target_relations:
                href = self._link_href(links, rel)
                if href:
                    break

            # 2. Fallback for systems that use the 'about' URI for Descriptions
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
        for widget in clickable_widgets:
            widget.bind("<Button-1>", lambda e, person=person_data: self.open_person_card(person))

        return card

    def open_person_card(self, person_data):
        href = self._link_href(person_data.get("links", {}), "person")
        if not href:
            href = self._link_href(person_data.get("links", {}), "self")

        if href:
            self.navigate_to(href, navigation_mode="new")
        else:
            self.show_notification(f"Could not open person: no person/self link was supplied.", "warning")

    def _link_href(self, links, relation):
        link = links.get(relation)
        if isinstance(link, dict):
            return link.get("href")
        if isinstance(link, list) and link and isinstance(link[0], dict):
            return link[0].get("href")
        return None

    def fetch_related_people(self, person_data, relation):
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
            return document.get("persons", [])
        except Exception as e:
            self.show_notification(f"Could not load {relation} for this person: {e}", "warning")
        return []

    def draw_3_generation_view(self, selected_person):
        self.clear_visual_canvas()

        parents = self.fetch_related_people(selected_person, "parents")
        children = self.fetch_related_people(selected_person, "children")

        outer_container = ttk.Frame(self.visual_scrollable_frame)
        outer_container.pack(expand=True, fill=tk.BOTH, padx=20, pady=20)

        # Generation 1: PARENTS
        gen1_frame = ttk.LabelFrame(outer_container, text="Parents", padding=10)
        gen1_frame.pack(fill=tk.X, pady=(0, 10))

        parents_inner = ttk.Frame(gen1_frame)
        parents_inner.pack(anchor=tk.CENTER)

        if parents:
            for p in parents:
                card = self.create_person_card(parents_inner, p, is_selected=False)
                card.pack(side=tk.LEFT, padx=10, pady=5)
        else:
            ttk.Label(parents_inner, text="No known parents for this person.", font=("Arial", 9, "italic")).pack(pady=5)

        # Generation 2: SELECTED PERSON
        gen2_frame = ttk.Frame(outer_container)
        gen2_frame.pack(fill=tk.X, pady=5)

        main_card = self.create_person_card(gen2_frame, selected_person, is_selected=True)
        main_card.pack(anchor=tk.CENTER, pady=5)

        # Generation 3: CHILDREN
        gen3_frame = ttk.LabelFrame(outer_container, text=f"Children ({len(children)})", padding=10)
        gen3_frame.pack(fill=tk.X, pady=(10, 0))

        if children:
            child_canvas = tk.Canvas(gen3_frame, height=170, bg="#f5f7fa", highlightthickness=0)
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
            ttk.Label(gen3_frame, text="No known children for this person.", font=("Arial", 9, "italic")).pack(anchor=tk.CENTER, pady=5)

if __name__ == "__main__":
    root = tk.Tk()
    app = GedcomXBrowserApp(root)
    root.mainloop()
